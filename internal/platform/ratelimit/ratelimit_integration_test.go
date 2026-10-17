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
	"github.com/kangsolder001/saka/internal/platform/ratelimit"
	"github.com/kangsolder001/saka/internal/platform/redistest"
	"github.com/kangsolder001/saka/internal/platform/redisx"
)

var epoch = time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

type env struct {
	redis   *redisx.Client
	clock   *clock.Fake
	limiter *ratelimit.Limiter
}

func newEnv(t *testing.T) *env {
	t.Helper()
	client := redistest.NewClient(t)
	clk := clock.NewFake(epoch)
	return &env{redis: client, clock: clk, limiter: ratelimit.New(client, clk)}
}

var threePerHour = ratelimit.Rule{Name: "test", Limit: 3, Window: time.Hour}

// events is how many events Redis holds for a key.
func (e *env) events(t *testing.T, key string) int64 {
	t.Helper()
	n, err := e.redis.Raw().ZCard(context.Background(), e.redis.Namespace(ratelimit.Module).Key(key)).Result()
	require.NoError(t, err)
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

	assert.EqualValues(t, 3, e.events(t, "k"), "hammering a locked key must not extend the lockout")
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
	for range 40 {
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

	assert.EqualValues(t, 3, allowed.Load(), "the Lua script makes check-and-add atomic")
	assert.EqualValues(t, 3, e.events(t, "hot-key"))
}

func TestTake_SameInstantEventsAreAllCounted(t *testing.T) {
	e := newEnv(t) // the fake clock does not move: three events share one timestamp
	ctx := context.Background()

	for range 3 {
		d, err := e.limiter.Take(ctx, "k", threePerHour)
		require.NoError(t, err)
		require.True(t, d.Allowed)
	}

	assert.EqualValues(t, 3, e.events(t, "k"), "events with equal timestamps must not overwrite each other")
}

func TestTake_DropsExpiredEventsOfTheSameKey(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, err := e.limiter.Take(ctx, "k", threePerHour)
	require.NoError(t, err)
	_, err = e.limiter.Take(ctx, "other", threePerHour)
	require.NoError(t, err)

	e.clock.Advance(2 * time.Hour)
	_, err = e.limiter.Take(ctx, "k", threePerHour)
	require.NoError(t, err)

	assert.EqualValues(t, 1, e.events(t, "k"), "the old event was removed and replaced")
	assert.EqualValues(t, 1, e.events(t, "other"), "an untouched key keeps its event")
}

func TestTake_KeysExpireOnTheirOwnSoRedisDoesNotFillUp(t *testing.T) {
	e := newEnv(t)
	_, err := e.limiter.Take(context.Background(), "k", threePerHour)
	require.NoError(t, err)

	ttl, err := e.redis.Raw().PTTL(context.Background(), e.redis.Namespace(ratelimit.Module).Key("k")).Result()

	require.NoError(t, err)
	assert.Positive(t, ttl)
	assert.LessOrEqual(t, ttl, time.Hour, "no limiter key may live longer than its window")
}

func TestKeysLiveInTheRateLimitNamespace(t *testing.T) {
	e := newEnv(t)
	key := ratelimit.Key("login", "ann@example.com", "203.0.113.9")
	_, err := e.limiter.Take(context.Background(), key, threePerHour)
	require.NoError(t, err)

	exists, err := e.redis.Raw().Exists(context.Background(), e.redis.Namespace("ratelimit").Key(key)).Result()
	require.NoError(t, err)

	assert.EqualValues(t, 1, exists, "stored as <key_prefix>:ratelimit:login:<hash>")
	assert.EqualValues(t, 0, e.events(t, "login"), "and nothing sits under the bare name")
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
	assert.Zero(t, e.events(t, "k"), "checking is read-only")

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

func TestRecord_SetsAnExpiry(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.limiter.Record(context.Background(), "k", threePerHour))

	ttl, err := e.redis.Raw().PTTL(context.Background(), e.redis.Namespace(ratelimit.Module).Key("k")).Result()

	require.NoError(t, err)
	assert.Positive(t, ttl)
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

func TestLimiterFailsClosedWhenRedisIsDown(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.redis.Close())

	_, takeErr := e.limiter.Take(context.Background(), "k", threePerHour)
	_, checkErr := e.limiter.Check(context.Background(), "k", threePerHour)

	assert.Error(t, takeErr, "an outage must never silently disable rate limiting")
	assert.Error(t, checkErr)
}
