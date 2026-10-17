// Package outbox implements the transactional outbox: services insert jobs in
// the same transaction as their data change, and a worker delivers them later
// (spec §10.1). Job types are generic; email is just one handler.
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/outbox/outboxdb"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

// NewJob describes a job to enqueue.
type NewJob struct {
	// Type selects the worker handler, e.g. "email.send".
	Type string
	// Payload is marshalled to JSON; nil becomes {}.
	Payload any
	// RunAt delays the first attempt; zero means as soon as possible.
	RunAt time.Time
}

// Enqueuer is what services depend on to schedule background work.
type Enqueuer interface {
	// Enqueue inserts the job using the caller's transaction, so the job
	// exists if and only if the caller's data change commits.
	Enqueue(ctx context.Context, tx db.DBTX, job NewJob) (uuid.UUID, error)
}

// Store persists outbox jobs. The pool is used by the worker; Enqueue always
// uses the transaction handed in by the caller.
type Store struct {
	pool db.DBTX
	clk  clock.Clock
}

var _ Enqueuer = (*Store)(nil)

// NewStore creates a Store backed by pool (used for claiming and finishing jobs).
func NewStore(pool db.DBTX, clk clock.Clock) *Store {
	return &Store{pool: pool, clk: clk}
}

// Enqueue implements Enqueuer. The trace ID in ctx is saved with the job so
// the worker's logs can be joined with the originating request.
func (s *Store) Enqueue(ctx context.Context, tx db.DBTX, job NewJob) (uuid.UUID, error) {
	if job.Type == "" {
		return uuid.Nil, errors.New("outbox: job type is required")
	}
	payload, err := marshalPayload(job.Payload)
	if err != nil {
		return uuid.Nil, fmt.Errorf("outbox: marshal payload for %q: %w", job.Type, err)
	}

	now := s.clk.Now()
	runAt := job.RunAt
	if runAt.IsZero() {
		runAt = now
	}

	var traceID *string
	if id := reqctx.TraceID(ctx); id != "" {
		traceID = &id
	}

	id, err := outboxdb.New(tx).InsertJob(ctx, outboxdb.InsertJobParams{
		Type:          job.Type,
		Payload:       payload,
		TraceID:       traceID,
		NextAttemptAt: runAt.UTC(),
		Now:           now,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("outbox: insert job %q: %w", job.Type, err)
	}
	return id, nil
}

func marshalPayload(p any) ([]byte, error) {
	if p == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(p)
}

// Job is a claimed job handed to a handler.
type Job struct {
	ID      uuid.UUID
	Type    string
	Payload json.RawMessage
	// Attempts counts executions including the current one (1 on the first run).
	Attempts int
	// TraceID is the trace of the request that enqueued the job, if any.
	TraceID   string
	CreatedAt time.Time
}

// ErrLeaseLost is returned when a job could not be updated because another
// worker reclaimed it after this worker's lease expired.
var ErrLeaseLost = errors.New("outbox: job lease lost")

const maxLastErrorLen = 1000

// Claim atomically takes up to limit due jobs for the calling worker. Jobs
// stuck in processing for longer than lease are reclaimed. Concurrent callers
// receive disjoint jobs (FOR UPDATE SKIP LOCKED).
func (s *Store) Claim(ctx context.Context, limit int, lease time.Duration) ([]Job, error) {
	now := s.clk.Now()
	rows, err := outboxdb.New(s.pool).ClaimJobs(ctx, outboxdb.ClaimJobsParams{
		Now:                now,
		LeaseExpiredBefore: now.Add(-lease),
		BatchSize:          int32(limit), //nolint:gosec // limit is a small configured batch size
	})
	if err != nil {
		return nil, fmt.Errorf("outbox: claim jobs: %w", err)
	}

	jobs := make([]Job, len(rows))
	for i, r := range rows {
		jobs[i] = Job{
			ID:        r.ID,
			Type:      r.Type,
			Payload:   r.Payload,
			Attempts:  int(r.Attempts),
			CreatedAt: r.CreatedAt,
		}
		if r.TraceID != nil {
			jobs[i].TraceID = *r.TraceID
		}
	}
	return jobs, nil
}

// Complete marks a claimed job as done.
func (s *Store) Complete(ctx context.Context, job Job) error {
	n, err := outboxdb.New(s.pool).MarkJobDone(ctx, outboxdb.MarkJobDoneParams{
		Now:      s.clk.Now(),
		ID:       job.ID,
		Attempts: int32(job.Attempts), //nolint:gosec // bounded by MaxAttempts
	})
	return finish("complete", n, err)
}

// Retry puts a claimed job back to pending, to run again at nextAttemptAt.
func (s *Store) Retry(ctx context.Context, job Job, nextAttemptAt time.Time, cause error) error {
	n, err := outboxdb.New(s.pool).ScheduleJobRetry(ctx, outboxdb.ScheduleJobRetryParams{
		Now:           s.clk.Now(),
		NextAttemptAt: nextAttemptAt.UTC(),
		LastError:     truncateError(cause),
		ID:            job.ID,
		Attempts:      int32(job.Attempts), //nolint:gosec // bounded by MaxAttempts
	})
	return finish("retry", n, err)
}

// Fail marks a claimed job as permanently failed for manual review.
func (s *Store) Fail(ctx context.Context, job Job, cause error) error {
	n, err := outboxdb.New(s.pool).MarkJobFailed(ctx, outboxdb.MarkJobFailedParams{
		Now:       s.clk.Now(),
		LastError: truncateError(cause),
		ID:        job.ID,
		Attempts:  int32(job.Attempts), //nolint:gosec // bounded by MaxAttempts
	})
	return finish("fail", n, err)
}

func finish(op string, rowsAffected int64, err error) error {
	if err != nil {
		return fmt.Errorf("outbox: %s job: %w", op, err)
	}
	if rowsAffected == 0 {
		return ErrLeaseLost
	}
	return nil
}

func truncateError(err error) *string {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if len(msg) > maxLastErrorLen {
		msg = msg[:maxLastErrorLen]
	}
	return &msg
}
