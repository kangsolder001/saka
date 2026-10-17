package outbox_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/outbox"
	"github.com/kangsolder001/saka/internal/platform/outbox/mocks"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
	"github.com/kangsolder001/saka/internal/platform/safego"
)

var now = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type workerFixture struct {
	worker *outbox.Worker
	queue  *mocks.MockQueue
	logs   *syncBuffer
}

func newWorker(t *testing.T, cfg outbox.WorkerConfig) *workerFixture {
	t.Helper()
	logs := &syncBuffer{}
	log, err := logger.New(logs, "debug")
	require.NoError(t, err)
	queue := mocks.NewMockQueue(t)

	w, err := outbox.NewWorker(queue, clock.NewFake(now), log, safego.NewRunner(log), cfg)
	require.NoError(t, err)
	return &workerFixture{worker: w, queue: queue, logs: logs}
}

func testConfig() outbox.WorkerConfig {
	cfg := outbox.DefaultWorkerConfig()
	cfg.Concurrency = 2
	return cfg
}

func job(attempts int) outbox.Job {
	return outbox.Job{ID: uuid.New(), Type: "email.send", Attempts: attempts, TraceID: "4bf92f3577b34da6a3ce929d0e0e4736"}
}

func (f *workerFixture) claims(jobs ...outbox.Job) {
	f.queue.EXPECT().Claim(mock.Anything, 2, 5*time.Minute).Return(jobs, nil).Once()
}

func TestNewWorker_ValidatesConfig(t *testing.T) {
	log := logger.Nop()
	tests := map[string]func(c *outbox.WorkerConfig){
		"no concurrency":       func(c *outbox.WorkerConfig) { c.Concurrency = 0 },
		"no poll interval":     func(c *outbox.WorkerConfig) { c.PollInterval = 0 },
		"no job timeout":       func(c *outbox.WorkerConfig) { c.JobTimeout = 0 },
		"lease below timeout":  func(c *outbox.WorkerConfig) { c.Lease = c.JobTimeout },
		"lease under timeout2": func(c *outbox.WorkerConfig) { c.Lease = time.Second },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := outbox.DefaultWorkerConfig()
			mutate(&cfg)

			_, err := outbox.NewWorker(mocks.NewMockQueue(t), clock.NewFake(now), log, safego.NewRunner(log), cfg)

			assert.Error(t, err)
		})
	}
}

func TestRunOnce_NoJobs(t *testing.T) {
	f := newWorker(t, testConfig())
	f.claims()

	n, err := f.worker.RunOnce(context.Background())

	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestRunOnce_ClaimErrorIsReturned(t *testing.T) {
	f := newWorker(t, testConfig())
	f.queue.EXPECT().Claim(mock.Anything, 2, 5*time.Minute).Return(nil, errors.New("db down")).Once()

	_, err := f.worker.RunOnce(context.Background())

	assert.EqualError(t, err, "db down")
}

func TestProcess_SuccessCompletesJob(t *testing.T) {
	f := newWorker(t, testConfig())
	j := job(1)
	f.claims(j)
	f.queue.EXPECT().Complete(mock.Anything, j).Return(nil).Once()
	f.worker.Register("email.send", func(context.Context, outbox.Job) error { return nil })

	n, err := f.worker.RunOnce(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Contains(t, f.logs.String(), `"message":"outbox job done"`)
}

func TestProcess_RestoresTraceIDIntoHandlerContext(t *testing.T) {
	f := newWorker(t, testConfig())
	j := job(1)
	f.claims(j)
	f.queue.EXPECT().Complete(mock.Anything, j).Return(nil).Once()
	var seenTrace, seenRequest string
	f.worker.Register("email.send", func(ctx context.Context, _ outbox.Job) error {
		seenTrace = reqctx.TraceID(ctx)
		seenRequest = reqctx.RequestID(ctx)
		return nil
	})

	_, err := f.worker.RunOnce(context.Background())

	require.NoError(t, err)
	assert.Equal(t, j.TraceID, seenTrace)
	assert.NotEmpty(t, seenRequest, "each attempt gets its own request id")
	assert.Contains(t, f.logs.String(), `"trace_id":"`+j.TraceID+`"`)
	assert.Contains(t, f.logs.String(), `"job_id":"`+j.ID.String()+`"`)
}

func TestProcess_JobWithoutTraceIDGetsFreshOne(t *testing.T) {
	f := newWorker(t, testConfig())
	j := job(1)
	j.TraceID = ""
	f.claims(j)
	f.queue.EXPECT().Complete(mock.Anything, j).Return(nil).Once()
	var seen string
	f.worker.Register("email.send", func(ctx context.Context, _ outbox.Job) error {
		seen = reqctx.TraceID(ctx)
		return nil
	})

	_, err := f.worker.RunOnce(context.Background())

	require.NoError(t, err)
	assert.Regexp(t, `^[0-9a-f]{32}$`, seen)
}

func TestProcess_FailureSchedulesRetryWithBackoff(t *testing.T) {
	delays := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}
	for i, delay := range delays {
		attempt := i + 1
		t.Run(delay.String(), func(t *testing.T) {
			f := newWorker(t, testConfig())
			j := job(attempt)
			boom := errors.New("smtp unavailable")
			f.claims(j)
			f.queue.EXPECT().Retry(mock.Anything, j, now.Add(delay), boom).Return(nil).Once()
			f.worker.Register("email.send", func(context.Context, outbox.Job) error { return boom })

			_, err := f.worker.RunOnce(context.Background())

			require.NoError(t, err)
			assert.Contains(t, f.logs.String(), `"message":"outbox job failed, will retry"`)
		})
	}
}

func TestProcess_FinalAttemptFailureMarksJobFailed(t *testing.T) {
	f := newWorker(t, testConfig())
	j := job(outbox.MaxAttempts)
	boom := errors.New("still failing")
	f.claims(j)
	f.queue.EXPECT().Fail(mock.Anything, j, boom).Return(nil).Once()
	f.worker.Register("email.send", func(context.Context, outbox.Job) error { return boom })

	_, err := f.worker.RunOnce(context.Background())

	require.NoError(t, err)
	assert.Contains(t, f.logs.String(), `"message":"outbox job failed permanently"`)
	assert.Contains(t, f.logs.String(), `"level":"error"`, "exhausted jobs must be visible in logs")
}

func TestProcess_PermanentErrorFailsWithoutRetry(t *testing.T) {
	f := newWorker(t, testConfig())
	j := job(1)
	cause := errors.New("template not found")
	f.claims(j)
	f.queue.EXPECT().Fail(mock.Anything, j, mock.MatchedBy(func(err error) bool {
		return errors.Is(err, cause) && outbox.IsPermanent(err)
	})).Return(nil).Once()
	f.worker.Register("email.send", func(context.Context, outbox.Job) error { return outbox.Permanent(cause) })

	_, err := f.worker.RunOnce(context.Background())

	require.NoError(t, err)
}

func TestProcess_UnknownJobTypeFailsPermanently(t *testing.T) {
	f := newWorker(t, testConfig())
	j := job(1)
	j.Type = "mystery"
	f.claims(j)
	f.queue.EXPECT().Fail(mock.Anything, j, mock.MatchedBy(func(err error) bool {
		return outbox.IsPermanent(err)
	})).Return(nil).Once()

	_, err := f.worker.RunOnce(context.Background())

	require.NoError(t, err)
}

func TestProcess_PanicIsTreatedAsRetryableFailure(t *testing.T) {
	f := newWorker(t, testConfig())
	j := job(1)
	f.claims(j)
	f.queue.EXPECT().Retry(mock.Anything, j, now.Add(time.Minute), mock.MatchedBy(func(err error) bool {
		var pe *safego.PanicError
		return errors.As(err, &pe)
	})).Return(nil).Once()
	f.worker.Register("email.send", func(context.Context, outbox.Job) error { panic("nil pointer") })

	_, err := f.worker.RunOnce(context.Background())

	require.NoError(t, err, "a panicking handler must not crash or fail the batch")
	assert.Contains(t, f.logs.String(), `"panic":"nil pointer"`)
}

func TestProcess_LeaseLostIsLoggedNotFatal(t *testing.T) {
	f := newWorker(t, testConfig())
	j := job(1)
	f.claims(j)
	f.queue.EXPECT().Complete(mock.Anything, j).Return(outbox.ErrLeaseLost).Once()
	f.worker.Register("email.send", func(context.Context, outbox.Job) error { return nil })

	_, err := f.worker.RunOnce(context.Background())

	require.NoError(t, err)
	assert.Contains(t, f.logs.String(), "lease lost")
}

func TestProcess_HandlerGetsJobTimeout(t *testing.T) {
	cfg := testConfig()
	cfg.JobTimeout = 30 * time.Millisecond
	cfg.Lease = time.Minute
	logs := &syncBuffer{}
	log, err := logger.New(logs, "debug")
	require.NoError(t, err)
	queue := mocks.NewMockQueue(t)
	w, err := outbox.NewWorker(queue, clock.NewFake(now), log, safego.NewRunner(log), cfg)
	require.NoError(t, err)
	j := job(1)
	queue.EXPECT().Claim(mock.Anything, 2, time.Minute).Return([]outbox.Job{j}, nil).Once()
	queue.EXPECT().Retry(mock.Anything, j, now.Add(time.Minute), mock.MatchedBy(func(err error) bool {
		return errors.Is(err, context.DeadlineExceeded)
	})).Return(nil).Once()
	w.Register("email.send", func(ctx context.Context, _ outbox.Job) error {
		<-ctx.Done()
		return ctx.Err()
	})

	_, err = w.RunOnce(context.Background())

	require.NoError(t, err)
}

func TestProcess_ClaimedJobsFinishEvenIfShutdownStarts(t *testing.T) {
	f := newWorker(t, testConfig())
	j := job(1)
	ctx, cancel := context.WithCancel(context.Background())
	f.claims(j)
	f.queue.EXPECT().Complete(mock.Anything, j).Return(nil).Once()
	f.worker.Register("email.send", func(handlerCtx context.Context, _ outbox.Job) error {
		cancel() // shutdown signal arrives mid-job
		select {
		case <-handlerCtx.Done():
			return handlerCtx.Err()
		case <-time.After(20 * time.Millisecond):
			return nil
		}
	})

	_, err := f.worker.RunOnce(ctx)

	require.NoError(t, err)
}

func TestRegister_DuplicateTypePanics(t *testing.T) {
	f := newWorker(t, testConfig())
	f.worker.Register("x", func(context.Context, outbox.Job) error { return nil })

	assert.Panics(t, func() {
		f.worker.Register("x", func(context.Context, outbox.Job) error { return nil })
	})
}

func TestRun_StopsOnContextCancelAndDrainsFullBatches(t *testing.T) {
	cfg := testConfig()
	cfg.PollInterval = 10 * time.Millisecond
	f := newWorker(t, cfg)
	a, b := job(1), job(1)
	ctx, cancel := context.WithCancel(context.Background())
	// A full batch is followed immediately by another claim; an empty one then waits.
	f.queue.EXPECT().Claim(mock.Anything, 2, 5*time.Minute).Return([]outbox.Job{a, b}, nil).Once()
	f.queue.EXPECT().Complete(mock.Anything, mock.Anything).Return(nil).Twice()
	f.queue.EXPECT().Claim(mock.Anything, 2, 5*time.Minute).RunAndReturn(
		func(context.Context, int, time.Duration) ([]outbox.Job, error) {
			cancel()
			return nil, nil
		}).Once()
	f.worker.Register("email.send", func(context.Context, outbox.Job) error { return nil })

	done := make(chan error, 1)
	go func() { done <- f.worker.Run(ctx) }()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
