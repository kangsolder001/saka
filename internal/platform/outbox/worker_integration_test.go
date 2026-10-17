//go:build integration

package outbox_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/outbox"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
	"github.com/kangsolder001/saka/internal/platform/safego"
)

func (e *env) newWorker(t *testing.T, logs *syncBuffer) *outbox.Worker {
	t.Helper()
	log, err := logger.New(logs, "debug")
	require.NoError(t, err)
	cfg := outbox.DefaultWorkerConfig()
	cfg.Concurrency = 3
	w, err := outbox.NewWorker(e.store, e.clock, log, safego.NewRunner(log), cfg)
	require.NoError(t, err)
	return w
}

func TestWorkers_TwoWorkersProcessEachJobExactlyOnce(t *testing.T) {
	e := newEnv(t)
	const total = 30
	for range total {
		e.enqueue(t, "email.send", time.Time{})
	}

	var (
		mu        sync.Mutex
		processed = map[string]int{}
	)
	handler := func(_ context.Context, j outbox.Job) error {
		time.Sleep(2 * time.Millisecond) // widen the race window
		mu.Lock()
		processed[j.ID.String()]++
		mu.Unlock()
		return nil
	}
	w1, w2 := e.newWorker(t, &syncBuffer{}), e.newWorker(t, &syncBuffer{})
	w1.Register("email.send", handler)
	w2.Register("email.send", handler)

	var wg sync.WaitGroup
	for _, w := range []*outbox.Worker{w1, w2} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				n, err := w.RunOnce(context.Background())
				if !assert.NoError(t, err) || n == 0 {
					return
				}
			}
		}()
	}
	wg.Wait()

	assert.Len(t, processed, total, "every job processed")
	for id, n := range processed {
		assert.Equal(t, 1, n, "job %s processed %d times", id, n)
	}
	for _, r := range e.jobs(t) {
		assert.Equal(t, "done", r.Status)
		assert.Equal(t, 1, r.Attempts)
	}
}

func TestWorker_RetriesFollowBackoffScheduleThenFails(t *testing.T) {
	e := newEnv(t)
	e.enqueue(t, "email.send", time.Time{})
	w := e.newWorker(t, &syncBuffer{})
	var calls atomic.Int32
	w.Register("email.send", func(context.Context, outbox.Job) error {
		calls.Add(1)
		return errors.New("smtp unavailable")
	})
	ctx := context.Background()

	delays := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}
	for i, delay := range delays {
		n, err := w.RunOnce(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, n, "attempt %d runs", i+1)

		row := e.jobs(t)[0]
		assert.Equal(t, "pending", row.Status, "after failure %d", i+1)
		assert.Equal(t, i+1, row.Attempts)
		assert.True(t, e.clock.Now().Add(delay).Equal(row.NextAttemptAt), "failure %d waits %s", i+1, delay)
		require.NotNil(t, row.LastError)
		assert.Equal(t, "smtp unavailable", *row.LastError)

		// One second early: nothing is due yet.
		e.clock.Advance(delay - time.Second)
		n, err = w.RunOnce(ctx)
		require.NoError(t, err)
		assert.Zero(t, n, "not due %s after failure %d", time.Second, i+1)
		e.clock.Advance(time.Second)
	}

	n, err := w.RunOnce(ctx) // sixth and final attempt
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.EqualValues(t, outbox.MaxAttempts, calls.Load())
	row := e.jobs(t)[0]
	assert.Equal(t, "failed", row.Status)
	assert.Equal(t, outbox.MaxAttempts, row.Attempts)

	e.clock.Advance(24 * time.Hour)
	n, err = w.RunOnce(ctx)
	require.NoError(t, err)
	assert.Zero(t, n, "failed jobs are never picked up again")
}

func TestWorker_SucceedsOnRetryAfterTransientFailure(t *testing.T) {
	e := newEnv(t)
	e.enqueue(t, "email.send", time.Time{})
	w := e.newWorker(t, &syncBuffer{})
	var calls atomic.Int32
	w.Register("email.send", func(context.Context, outbox.Job) error {
		if calls.Add(1) == 1 {
			return errors.New("temporary")
		}
		return nil
	})
	ctx := context.Background()

	_, err := w.RunOnce(ctx)
	require.NoError(t, err)
	e.clock.Advance(time.Minute)
	_, err = w.RunOnce(ctx)
	require.NoError(t, err)

	row := e.jobs(t)[0]
	assert.Equal(t, "done", row.Status)
	assert.Equal(t, 2, row.Attempts)
	assert.Nil(t, row.LastError, "a successful retry clears the previous error")
}

func TestWorker_PermanentErrorFailsImmediately(t *testing.T) {
	e := newEnv(t)
	e.enqueue(t, "email.send", time.Time{})
	w := e.newWorker(t, &syncBuffer{})
	w.Register("email.send", func(context.Context, outbox.Job) error {
		return outbox.Permanent(errors.New("unknown template"))
	})

	_, err := w.RunOnce(context.Background())

	require.NoError(t, err)
	row := e.jobs(t)[0]
	assert.Equal(t, "failed", row.Status)
	assert.Equal(t, 1, row.Attempts)
}

func TestWorker_TraceIDFlowsFromRequestToJobHandlerAndLogs(t *testing.T) {
	e := newEnv(t)
	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	reqCtx := reqctx.WithTraceID(context.Background(), traceID)
	require.NoError(t, e.db.WithTx(reqCtx, func(tx pgx.Tx) error {
		_, err := e.store.Enqueue(reqCtx, tx, outbox.NewJob{Type: "email.send"})
		return err
	}))
	logs := &syncBuffer{}
	w := e.newWorker(t, logs)
	var seen string
	w.Register("email.send", func(ctx context.Context, _ outbox.Job) error {
		seen = reqctx.TraceID(ctx)
		return nil
	})

	// The worker runs in a different "process": its context has no trace.
	_, err := w.RunOnce(context.Background())

	require.NoError(t, err)
	assert.Equal(t, traceID, seen)
	assert.Contains(t, logs.String(), `"trace_id":"`+traceID+`"`)
}

func TestWorker_CrashedWorkerJobIsTakenOverAfterLease(t *testing.T) {
	e := newEnv(t)
	e.enqueue(t, "email.send", time.Time{})
	// A worker claims the job and then dies without recording an outcome.
	claimed, err := e.store.Claim(context.Background(), 1, outbox.DefaultWorkerConfig().Lease)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	w := e.newWorker(t, &syncBuffer{})
	var calls atomic.Int32
	w.Register("email.send", func(context.Context, outbox.Job) error { calls.Add(1); return nil })

	n, err := w.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Zero(t, n, "lease still held by the dead worker")

	e.clock.Advance(outbox.DefaultWorkerConfig().Lease + time.Second)
	n, err = w.RunOnce(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 1, n)
	assert.EqualValues(t, 1, calls.Load())
	row := e.jobs(t)[0]
	assert.Equal(t, "done", row.Status)
	assert.Equal(t, 2, row.Attempts)
}

func TestWorker_RunProcessesJobsUntilCancelled(t *testing.T) {
	e := newEnv(t)
	for range 5 {
		e.enqueue(t, "email.send", time.Time{})
	}
	w := e.newWorker(t, &syncBuffer{})
	var done atomic.Int32
	w.Register("email.send", func(context.Context, outbox.Job) error { done.Add(1); return nil })
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- w.Run(ctx) }()
	require.Eventually(t, func() bool { return done.Load() == 5 }, 5*time.Second, 20*time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop after cancel")
	}
}
