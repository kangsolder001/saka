//go:build integration

package scheduler_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/dbtest"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/safego"
	"github.com/kangsolder001/saka/internal/platform/scheduler"
)

// worker is one worker process: its own pool, its own scheduler.
func worker(t *testing.T, dsn string) (*scheduler.Scheduler, *pgxpool.Pool) {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	log := logger.Nop()
	return scheduler.New(scheduler.NewPostgresLocker(pool), clock.Real{}, safego.NewRunner(log), log), pool
}

func TestPostgresLocker_OnlyOneWorkerRunsAJobAtATime(t *testing.T) {
	dsn := dbtest.NewDatabase(t)
	one, _ := worker(t, dsn)
	two, _ := worker(t, dsn)
	started, finish := make(chan struct{}), make(chan struct{})
	one.Register(scheduler.Job{Name: "billing.expire", Interval: time.Hour, Run: func(context.Context) (scheduler.Counts, error) {
		close(started)
		<-finish
		return scheduler.Counts{"done": 1}, nil
	}})
	var ranOnTwo int
	two.Register(scheduler.Job{Name: "billing.expire", Interval: time.Hour, Run: func(context.Context) (scheduler.Counts, error) {
		ranOnTwo++
		return nil, nil
	}})
	done := make(chan scheduler.RunResult, 1)
	go func() { // a test helper goroutine, not application code
		res, err := one.RunOnce(context.Background(), "billing.expire")
		assert.NoError(t, err)
		done <- res
	}()
	<-started

	res, err := two.RunOnce(context.Background(), "billing.expire")

	require.NoError(t, err)
	assert.False(t, res.Ran, "the second worker skips the job while the first runs it")
	assert.Zero(t, ranOnTwo)

	close(finish)
	assert.True(t, (<-done).Ran)
	res, err = two.RunOnce(context.Background(), "billing.expire")
	require.NoError(t, err)
	assert.True(t, res.Ran, "the lock was released when the first run ended")
	assert.Equal(t, 1, ranOnTwo)
}

func TestPostgresLocker_DifferentJobsDoNotBlockEachOther(t *testing.T) {
	dsn := dbtest.NewDatabase(t)
	one, _ := worker(t, dsn)
	two, _ := worker(t, dsn)
	started, finish := make(chan struct{}), make(chan struct{})
	one.Register(scheduler.Job{Name: "billing.reconcile", Interval: time.Hour, Run: func(context.Context) (scheduler.Counts, error) {
		close(started)
		<-finish
		return nil, nil
	}})
	two.Register(scheduler.Job{Name: "billing.renewals", Interval: time.Hour, Run: func(context.Context) (scheduler.Counts, error) { return nil, nil }})
	done := make(chan struct{})
	go func() { // a test helper goroutine, not application code
		defer close(done)
		_, err := one.RunOnce(context.Background(), "billing.reconcile")
		assert.NoError(t, err)
	}()
	<-started

	res, err := two.RunOnce(context.Background(), "billing.renewals")

	require.NoError(t, err)
	assert.True(t, res.Ran)
	close(finish)
	<-done
}

func TestPostgresLocker_LeavesNoConnectionBehind(t *testing.T) {
	dsn := dbtest.NewDatabase(t)
	s, pool := worker(t, dsn)
	s.Register(scheduler.Job{Name: "ok", Interval: time.Hour, Run: func(context.Context) (scheduler.Counts, error) { return nil, nil }})
	s.Register(scheduler.Job{Name: "panics", Interval: time.Hour, Run: func(context.Context) (scheduler.Counts, error) { panic("boom") }})

	for range 3 {
		_, err := s.RunOnce(context.Background(), "ok")
		require.NoError(t, err)
		_, err = s.RunOnce(context.Background(), "panics")
		require.Error(t, err)
	}

	assert.Zero(t, pool.Stat().AcquiredConns(), "every lock transaction was ended, even after a panic")
	var held int
	require.NoError(t, pool.QueryRow(context.Background(), "SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND database = (SELECT oid FROM pg_database WHERE datname = current_database())").Scan(&held))
	assert.Zero(t, held)
}

func TestPostgresLocker_ALockIsFreedWhenTheHoldingConnectionDies(t *testing.T) {
	dsn := dbtest.NewDatabase(t)
	holder, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(holder.Close)
	locker := scheduler.NewPostgresLocker(holder)
	key := scheduler.LockKey("billing.expire")
	release, ok, err := locker.TryLock(context.Background(), key)
	require.NoError(t, err)
	require.True(t, ok)
	t.Cleanup(release) // runs before holder.Close, which would wait for the held connection

	// A second connection pool is another process: it cannot take the lock...
	other, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	defer other.Close()
	_, ok, err = scheduler.NewPostgresLocker(other).TryLock(context.Background(), key)
	require.NoError(t, err)
	assert.False(t, ok)

	// ...until the holder's backend is terminated, as when a worker crashes.
	admin := dbtest.AdminPool(t, dsn)
	_, err = admin.Exec(context.Background(), `
		SELECT pg_terminate_backend(l.pid) FROM pg_locks l
		WHERE l.locktype = 'advisory' AND l.granted AND l.database = (SELECT oid FROM pg_database WHERE datname = current_database())`)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		release, ok, err := scheduler.NewPostgresLocker(other).TryLock(context.Background(), key)
		if err == nil && ok {
			release()
		}
		return err == nil && ok
	}, 5*time.Second, 50*time.Millisecond, "PostgreSQL released the lock of the dead connection")
}
