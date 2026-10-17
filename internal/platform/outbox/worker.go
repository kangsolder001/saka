package outbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
	"github.com/kangsolder001/saka/internal/platform/safego"
)

// Queue is the storage the worker needs; *Store implements it.
type Queue interface {
	Claim(ctx context.Context, limit int, lease time.Duration) ([]Job, error)
	Complete(ctx context.Context, job Job) error
	Retry(ctx context.Context, job Job, nextAttemptAt time.Time, cause error) error
	Fail(ctx context.Context, job Job, cause error) error
}

var _ Queue = (*Store)(nil)

// Handler processes one job. Return nil on success, an ordinary error to be
// retried with backoff, or Permanent(err) to fail the job without retrying.
// Handlers must be idempotent: delivery is at-least-once.
type Handler func(ctx context.Context, job Job) error

type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// Permanent marks err as not worth retrying (for example a malformed payload).
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

// IsPermanent reports whether err was marked with Permanent.
func IsPermanent(err error) bool {
	var pe *permanentError
	return errors.As(err, &pe)
}

// WorkerConfig tunes polling and concurrency.
type WorkerConfig struct {
	// Concurrency is the maximum number of jobs processed at the same time
	// and the batch size of each poll.
	Concurrency int
	// PollInterval is how long to sleep when no full batch was found.
	PollInterval time.Duration
	// JobTimeout bounds one handler call.
	JobTimeout time.Duration
	// Lease is how long a claimed job stays reserved; after that another
	// worker may take it over. Must be longer than JobTimeout.
	Lease time.Duration
}

// DefaultWorkerConfig returns sensible defaults.
func DefaultWorkerConfig() WorkerConfig {
	return WorkerConfig{
		Concurrency:  4,
		PollInterval: 5 * time.Second,
		JobTimeout:   time.Minute,
		Lease:        5 * time.Minute,
	}
}

func (c WorkerConfig) validate() error {
	switch {
	case c.Concurrency < 1:
		return errors.New("concurrency must be at least 1")
	case c.PollInterval <= 0:
		return errors.New("poll interval must be positive")
	case c.JobTimeout <= 0:
		return errors.New("job timeout must be positive")
	case c.Lease <= c.JobTimeout:
		return errors.New("lease must be longer than the job timeout")
	}
	return nil
}

// Worker polls the outbox and dispatches jobs to handlers by type.
type Worker struct {
	queue    Queue
	clk      clock.Clock
	log      *logger.Logger
	runner   *safego.Runner
	cfg      WorkerConfig
	handlers map[string]Handler
}

// NewWorker creates a Worker. Register handlers before calling Run.
func NewWorker(queue Queue, clk clock.Clock, log *logger.Logger, runner *safego.Runner, cfg WorkerConfig) (*Worker, error) {
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("outbox worker config: %w", err)
	}
	return &Worker{
		queue:    queue,
		clk:      clk,
		log:      log.Component("outbox-worker"),
		runner:   runner,
		cfg:      cfg,
		handlers: map[string]Handler{},
	}, nil
}

// Register sets the handler for a job type. It panics on a duplicate type
// because that is a wiring bug, and must not be called after Run starts.
func (w *Worker) Register(jobType string, h Handler) {
	if _, dup := w.handlers[jobType]; dup {
		panic(fmt.Sprintf("outbox: handler already registered for %q", jobType))
	}
	w.handlers[jobType] = h
}

// Run polls until ctx is cancelled. On cancellation it stops claiming new
// jobs but lets the jobs already claimed finish (bounded by JobTimeout)
// before returning, so a deploy does not abandon half-done work.
func (w *Worker) Run(ctx context.Context) error {
	base := w.log.Base()
	base.Info().
		Int("concurrency", w.cfg.Concurrency).
		Dur("poll_interval_ms", w.cfg.PollInterval).
		Msg("outbox worker started")

	for ctx.Err() == nil {
		n, err := w.RunOnce(ctx)
		if err != nil && ctx.Err() == nil {
			base.Error().Err(err).Msg("outbox poll failed")
		}
		if err == nil && n == w.cfg.Concurrency {
			continue // full batch: more work is probably waiting
		}
		w.sleep(ctx, w.cfg.PollInterval)
	}

	base.Info().Msg("outbox worker stopped")
	return nil
}

func (w *Worker) sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// RunOnce claims one batch and processes it to completion, returning how
// many jobs were claimed. Run calls it in a loop; tests call it directly.
func (w *Worker) RunOnce(ctx context.Context) (int, error) {
	jobs, err := w.queue.Claim(ctx, w.cfg.Concurrency, w.cfg.Lease)
	if err != nil {
		return 0, err
	}
	if len(jobs) == 0 {
		return 0, nil
	}

	// Claimed jobs finish even if shutdown starts now.
	runCtx := context.WithoutCancel(ctx)
	group := w.runner.NewGroup(runCtx, w.cfg.Concurrency)
	for _, job := range jobs {
		group.Go("outbox-job:"+job.Type, func(ctx context.Context) error {
			w.process(ctx, job)
			return nil // outcomes are recorded per job, never fail the batch
		})
	}
	if err := group.Wait(); err != nil {
		return len(jobs), err
	}
	return len(jobs), nil
}

func (w *Worker) process(ctx context.Context, job Job) {
	traceID := job.TraceID
	if traceID == "" {
		traceID = reqctx.NewTraceID()
	}
	ctx = reqctx.WithTraceID(ctx, traceID)
	ctx = reqctx.WithRequestID(ctx, reqctx.NewRequestID())

	zl := w.log.FromContext(ctx)
	zl = zl.With().
		Str("job_id", job.ID.String()).
		Str("job_type", job.Type).
		Int("attempt", job.Attempts).
		Logger()

	handler, ok := w.handlers[job.Type]
	if !ok {
		err := Permanent(fmt.Errorf("no handler registered for job type %q", job.Type))
		w.recordFailure(ctx, &zl, job, err)
		return
	}

	handlerCtx, cancel := context.WithTimeout(ctx, w.cfg.JobTimeout)
	defer cancel()
	err := w.runner.Do(handlerCtx, "outbox-handler:"+job.Type, func(ctx context.Context) error {
		return handler(ctx, job)
	})

	if err != nil {
		w.recordFailure(ctx, &zl, job, err)
		return
	}
	if err := w.queue.Complete(ctx, job); err != nil {
		w.logStoreError(&zl, "complete", err)
		return
	}
	zl.Info().Msg("outbox job done")
}

func (w *Worker) recordFailure(ctx context.Context, zl *zerolog.Logger, job Job, cause error) {
	delay, canRetry := RetryDelay(job.Attempts)
	if IsPermanent(cause) || !canRetry {
		if err := w.queue.Fail(ctx, job, cause); err != nil {
			w.logStoreError(zl, "fail", err)
			return
		}
		zl.Error().Err(cause).Bool("permanent", IsPermanent(cause)).Msg("outbox job failed permanently")
		return
	}

	next := w.clk.Now().Add(delay)
	if err := w.queue.Retry(ctx, job, next, cause); err != nil {
		w.logStoreError(zl, "retry", err)
		return
	}
	zl.Warn().Err(cause).Time("next_attempt_at", next).Msg("outbox job failed, will retry")
}

func (w *Worker) logStoreError(zl *zerolog.Logger, op string, err error) {
	if errors.Is(err, ErrLeaseLost) {
		zl.Warn().Str("op", op).Msg("outbox job lease lost; another worker took over")
		return
	}
	// The job stays in processing and is reclaimed when its lease expires.
	zl.Error().Err(err).Str("op", op).Msg("could not record outbox job outcome")
}
