//go:build integration

package ratelimit_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/dbtest"
	"github.com/kangsolder001/saka/internal/platform/ratelimit"
)

var epoch = time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

type env struct {
	db      *db.DB
	clock   *clock.Fake
	limiter *ratelimit.Limiter
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dsn := dbtest.NewDatabase(t)
	dbtest.Migrate(t, dsn)
	cfg := config.Default()
	cfg.Database.URL = dsn
	d, err := db.Connect(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(d.Close)
	clk := clock.NewFake(epoch)
	return &env{db: d, clock: clk, limiter: ratelimit.New(d, d.Pool(), clk)}
}

var threePerHour = ratelimit.Rule{Name: "test", Limit: 3, Window: time.Hour}

func (e *env) rows(t *testing.T) int {
	t.Helper()
	var n int
	require.NoError(t, e.db.Pool().QueryRow(context.Background(), "SELECT count(*) FROM rate_limit_hits").Scan(&n))
	return n
}

func TestTake_AllowsUpToTheLimitThenDenies(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		d, err := e.limiter.Take(ctx, "k", threePerHour)
		require.NoError(t, err)
		assert.True(t, d.Allowed, "attempt %d", i)
		assert.Equal(t, 3-i, d.Remaining)
	}
	d, err := e.limiter.Take(ctx, "k", threePerHour)

	require.NoError(t, err)
	assert.False(t, d.Allowed)
	assert.Zero(t, d.Remaining)
	assert.Equal(t, time.Hour, d.RetryAfter, "all three hits are at t0, so the wait is a full window")
}

func TestTake_DeniedAttemptsAreNotCounted(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for range 3 {
		_, err := e.limiter.Take(ctx, "k", threePerHour)
		require.NoError(t, err)
	}

	for range 10 {
		d, err := e.limiter.Take(ctx, "k", threePerHour)
		require.NoError(t, err)
		require.False(t, d.Allowed)
	}

	assert.Equal(t, 3, e.rows(t), "hammering a locked key must not extend the lockout")
}

func TestTake_WindowSlides(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, err := e.limiter.Take(ctx, "k", threePerHour) // t0
	require.NoError(t, err)
	e.clock.Advance(20 * time.Minute)
	_, err = e.limiter.Take(ctx, "k", threePerHour) // t0+20m
	require.NoError(t, err)
	e.clock.Advance(20 * time.Minute)
	_, err = e.limiter.Take(ctx, "k", threePerHour) // t0+40m
	require.NoError(t, err)

	d, err := e.limiter.Take(ctx, "k", threePerHour)
	require.NoError(t, err)
	assert.False(t, d.Allowed)
	assert.Equal(t, 20*time.Minute, d.RetryAfter, "the oldest hit (t0) leaves the window in 20 minutes")

	e.clock.Advance(20*time.Minute + time.Second) // t0 has now left the window
	d, err = e.limiter.Take(ctx, "k", threePerHour)
	require.NoError(t, err)
	assert.True(t, d.Allowed, "only one slot frees up as the window slides")
	d, err = e.limiter.Take(ctx, "k", threePerHour)
	require.NoError(t, err)
	assert.False(t, d.Allowed, "the other hits are still inside the window")
}

func TestTake_KeysAreIndependent(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for range 3 {
		_, err := e.limiter.Take(ctx, "alice", threePerHour)
		require.NoError(t, err)
	}

	d, err := e.limiter.Take(ctx, "bob", threePerHour)

	require.NoError(t, err)
	assert.True(t, d.Allowed)
}

func TestTake_ConcurrentCallersNeverExceedTheLimit(t *testing.T) {
	e := newEnv(t)
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for range 25 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := e.limiter.Take(context.Background(), "hot-key", threePerHour)
			if assert.NoError(t, err) && d.Allowed {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()

	assert.EqualValues(t, 3, allowed.Load(), "check-then-insert must be atomic per key")
	assert.Equal(t, 3, e.rows(t))
}

func TestTake_DropsExpiredHitsOfTheSameKey(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, err := e.limiter.Take(ctx, "k", threePerHour)
	require.NoError(t, err)
	_, err = e.limiter.Take(ctx, "other", threePerHour)
	require.NoError(t, err)

	e.clock.Advance(2 * time.Hour)
	_, err = e.limiter.Take(ctx, "k", threePerHour)
	require.NoError(t, err)

	assert.Equal(t, 2, e.rows(t), "k's old hit was removed and replaced; the untouched key keeps its row")
}

func TestCheck_DoesNotCountAndRecordDoes(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	for range 5 {
		d, err := e.limiter.Check(ctx, "k", threePerHour)
		require.NoError(t, err)
		require.True(t, d.Allowed)
		require.Equal(t, 3, d.Remaining)
	}
	assert.Zero(t, e.rows(t), "checking is read-only")

	for range 3 {
		require.NoError(t, e.limiter.Record(ctx, "k", threePerHour))
	}
	d, err := e.limiter.Check(ctx, "k", threePerHour)
	require.NoError(t, err)
	assert.False(t, d.Allowed)
	assert.Equal(t, time.Hour, d.RetryAfter)

	e.clock.Advance(time.Hour + time.Second)
	d, err = e.limiter.Check(ctx, "k", threePerHour)
	require.NoError(t, err)
	assert.True(t, d.Allowed, "the lockout ends when the window passes")
}

func TestReset_ForgetsOnlyThatKey(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for range 3 {
		require.NoError(t, e.limiter.Record(ctx, "locked", threePerHour))
		require.NoError(t, e.limiter.Record(ctx, "other", threePerHour))
	}

	require.NoError(t, e.limiter.Reset(ctx, "locked"))

	d, err := e.limiter.Check(ctx, "locked", threePerHour)
	require.NoError(t, err)
	assert.True(t, d.Allowed)
	d, err = e.limiter.Check(ctx, "other", threePerHour)
	require.NoError(t, err)
	assert.False(t, d.Allowed)
}

func TestRetryAfterIsAtLeastOneSecond(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	rule := ratelimit.Rule{Name: "tiny", Limit: 1, Window: time.Minute}
	_, err := e.limiter.Take(ctx, "k", rule)
	require.NoError(t, err)
	e.clock.Advance(time.Minute - 100*time.Millisecond)

	d, err := e.limiter.Take(ctx, "k", rule)

	require.NoError(t, err)
	assert.False(t, d.Allowed)
	assert.Equal(t, time.Second, d.RetryAfter, "never advertise Retry-After: 0")
}
