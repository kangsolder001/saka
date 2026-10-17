//go:build integration

package outbox_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/outbox"
)

const lease = 5 * time.Minute

func (e *env) enqueue(t *testing.T, jobType string, runAt time.Time) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, e.db.WithTx(ctx, func(tx pgx.Tx) error {
		_, err := e.store.Enqueue(ctx, tx, outbox.NewJob{Type: jobType, RunAt: runAt})
		return err
	}))
}

func TestClaim_TakesOnlyDueJobsAndMarksThemProcessing(t *testing.T) {
	e := newEnv(t)
	e.enqueue(t, "due", time.Time{})
	e.enqueue(t, "later", epoch.Add(time.Hour))

	jobs, err := e.store.Claim(context.Background(), 10, lease)

	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Equal(t, "due", jobs[0].Type)
	assert.Equal(t, 1, jobs[0].Attempts)
	statuses := map[string]string{}
	for _, r := range e.jobs(t) {
		statuses[r.Type] = r.Status
	}
	assert.Equal(t, map[string]string{"due": "processing", "later": "pending"}, statuses)
}

func TestClaim_BecomesDueWhenClockAdvances(t *testing.T) {
	e := newEnv(t)
	e.enqueue(t, "later", epoch.Add(time.Hour))

	before, err := e.store.Claim(context.Background(), 10, lease)
	require.NoError(t, err)
	e.clock.Advance(time.Hour)
	after, err := e.store.Claim(context.Background(), 10, lease)
	require.NoError(t, err)

	assert.Empty(t, before)
	assert.Len(t, after, 1)
}

func TestClaim_RespectsLimitAndOrdersByDueTime(t *testing.T) {
	e := newEnv(t)
	e.enqueue(t, "second", epoch.Add(-time.Minute))
	e.enqueue(t, "first", epoch.Add(-time.Hour))
	e.enqueue(t, "third", epoch)

	jobs, err := e.store.Claim(context.Background(), 2, lease)

	require.NoError(t, err)
	require.Len(t, jobs, 2)
	assert.Equal(t, "first", jobs[0].Type)
	assert.Equal(t, "second", jobs[1].Type)
}

func TestClaim_DoesNotReturnAlreadyClaimedJob(t *testing.T) {
	e := newEnv(t)
	e.enqueue(t, "x", time.Time{})

	first, err := e.store.Claim(context.Background(), 10, lease)
	require.NoError(t, err)
	second, err := e.store.Claim(context.Background(), 10, lease)
	require.NoError(t, err)

	assert.Len(t, first, 1)
	assert.Empty(t, second)
}

func TestClaim_ConcurrentWorkersGetDisjointJobs(t *testing.T) {
	e := newEnv(t)
	const total = 40
	for range total {
		e.enqueue(t, "x", time.Time{})
	}

	var (
		mu      sync.Mutex
		seen    = map[string]int{}
		wg      sync.WaitGroup
		claimed int
	)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				jobs, err := e.store.Claim(context.Background(), 3, lease)
				if !assert.NoError(t, err) || len(jobs) == 0 {
					return
				}
				mu.Lock()
				for _, j := range jobs {
					seen[j.ID.String()]++
					claimed++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, total, claimed)
	assert.Len(t, seen, total, "every job claimed")
	for id, n := range seen {
		assert.Equal(t, 1, n, "job %s claimed more than once", id)
	}
}

func TestClaim_ReclaimsJobsWhoseLeaseExpired(t *testing.T) {
	e := newEnv(t)
	e.enqueue(t, "x", time.Time{})
	first, err := e.store.Claim(context.Background(), 10, lease)
	require.NoError(t, err)
	require.Len(t, first, 1)

	e.clock.Advance(lease - time.Second)
	notYet, err := e.store.Claim(context.Background(), 10, lease)
	require.NoError(t, err)
	e.clock.Advance(2 * time.Second)
	reclaimed, err := e.store.Claim(context.Background(), 10, lease)
	require.NoError(t, err)

	assert.Empty(t, notYet, "lease still valid")
	require.Len(t, reclaimed, 1)
	assert.Equal(t, 2, reclaimed[0].Attempts, "a crashed attempt counts as an attempt")
}

func TestComplete_MarksDone(t *testing.T) {
	e := newEnv(t)
	e.enqueue(t, "x", time.Time{})
	jobs, err := e.store.Claim(context.Background(), 1, lease)
	require.NoError(t, err)

	require.NoError(t, e.store.Complete(context.Background(), jobs[0]))

	assert.Equal(t, "done", e.jobs(t)[0].Status)
	again, err := e.store.Claim(context.Background(), 10, lease)
	require.NoError(t, err)
	assert.Empty(t, again)
}

func TestRetry_SchedulesPendingJobWithError(t *testing.T) {
	e := newEnv(t)
	e.enqueue(t, "x", time.Time{})
	jobs, err := e.store.Claim(context.Background(), 1, lease)
	require.NoError(t, err)
	next := epoch.Add(time.Minute)

	require.NoError(t, e.store.Retry(context.Background(), jobs[0], next, errors.New("smtp down")))

	row := e.jobs(t)[0]
	assert.Equal(t, "pending", row.Status)
	assert.True(t, next.Equal(row.NextAttemptAt))
	require.NotNil(t, row.LastError)
	assert.Equal(t, "smtp down", *row.LastError)
	assert.Equal(t, 1, row.Attempts)
}

func TestFail_MarksFailedAndTruncatesLongErrors(t *testing.T) {
	e := newEnv(t)
	e.enqueue(t, "x", time.Time{})
	jobs, err := e.store.Claim(context.Background(), 1, lease)
	require.NoError(t, err)
	long := make([]byte, 5000)
	for i := range long {
		long[i] = 'e'
	}

	require.NoError(t, e.store.Fail(context.Background(), jobs[0], errors.New(string(long))))

	row := e.jobs(t)[0]
	assert.Equal(t, "failed", row.Status)
	require.NotNil(t, row.LastError)
	assert.Len(t, *row.LastError, 1000)
}

func TestFinish_StaleWorkerCannotOverwriteNewerAttempt(t *testing.T) {
	e := newEnv(t)
	e.enqueue(t, "x", time.Time{})
	stale, err := e.store.Claim(context.Background(), 1, lease)
	require.NoError(t, err)
	e.clock.Advance(lease + time.Second)
	fresh, err := e.store.Claim(context.Background(), 1, lease)
	require.NoError(t, err)
	require.Len(t, fresh, 1)

	assert.ErrorIs(t, e.store.Complete(context.Background(), stale[0]), outbox.ErrLeaseLost)
	assert.ErrorIs(t, e.store.Fail(context.Background(), stale[0], errors.New("late")), outbox.ErrLeaseLost)
	assert.ErrorIs(t, e.store.Retry(context.Background(), stale[0], epoch, errors.New("late")), outbox.ErrLeaseLost)
	assert.Equal(t, "processing", e.jobs(t)[0].Status, "the fresh attempt still owns the job")

	require.NoError(t, e.store.Complete(context.Background(), fresh[0]))
	assert.Equal(t, "done", e.jobs(t)[0].Status)
}

func TestClaim_CarriesTraceIDFromEnqueue(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	require.NoError(t, e.db.WithTx(ctx, func(tx pgx.Tx) error {
		_, err := e.store.Enqueue(traceCtx("4bf92f3577b34da6a3ce929d0e0e4736"), tx, outbox.NewJob{Type: "x"})
		return err
	}))

	jobs, err := e.store.Claim(ctx, 1, lease)

	require.NoError(t, err)
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", jobs[0].TraceID)
}
