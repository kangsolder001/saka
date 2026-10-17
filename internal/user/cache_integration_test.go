//go:build integration

package user_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/dbtest"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/redistest"
	"github.com/kangsolder001/saka/internal/platform/redisx"
	"github.com/kangsolder001/saka/internal/user"
)

const cacheTTL = 5 * time.Minute

type cacheEnv struct {
	db    *db.DB
	redis *redisx.Client
	clock *clock.Fake
	svc   *user.Service
}

func newCacheEnv(t *testing.T) *cacheEnv {
	t.Helper()
	dsn := dbtest.NewDatabase(t)
	dbtest.Migrate(t, dsn)
	cfg := config.Default()
	cfg.Database.URL = dsn
	d, err := db.Connect(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(d.Close)

	rdb := redistest.NewClient(t)
	clk := clock.NewFake(epoch)
	svc := user.NewService(clk, user.WithCache(rdb.Namespace("user"), cacheTTL, logger.Nop()))
	return &cacheEnv{db: d, redis: rdb, clock: clk, svc: svc}
}

func (e *cacheEnv) create(t *testing.T, email string) user.User {
	t.Helper()
	u, err := e.svc.Create(context.Background(), e.db.Pool(), user.NewUser{Email: email, PasswordHash: "secret-hash", Name: "Ann"})
	require.NoError(t, err)
	return u
}

func (e *cacheEnv) cachedRaw(ctx context.Context, t *testing.T, id uuid.UUID) ([]byte, bool) {
	t.Helper()
	raw, found, err := e.redis.Namespace("user").Get(ctx, "summary:v1:"+id.String())
	require.NoError(t, err)
	return raw, found
}

// updateBehindTheCacheBack changes the row directly, like an update that
// forgot to invalidate, to show what the cache is (and is not) serving.
func (e *cacheEnv) updateBehindTheCacheBack(t *testing.T, id uuid.UUID, sql string) {
	t.Helper()
	_, err := e.db.Pool().Exec(context.Background(), sql, id)
	require.NoError(t, err)
}

func TestGetSummary_FirstReadFillsTheCacheSecondReadComesFromIt(t *testing.T) {
	e := newCacheEnv(t)
	u := e.create(t, "ann@example.com")
	ctx := context.Background()
	_, cached := e.cachedRaw(context.Background(), t, u.ID)
	require.False(t, cached, "nothing is cached before the first read")

	first, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
	require.NoError(t, err)
	_, cached = e.cachedRaw(context.Background(), t, u.ID)
	assert.True(t, cached, "a miss fills the cache")

	// Change the row without invalidating: a cache hit must still return the old value.
	e.updateBehindTheCacheBack(t, u.ID, "UPDATE users SET name = 'Changed' WHERE id = $1")
	second, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)

	require.NoError(t, err)
	assert.Equal(t, "Ann", first.Name)
	assert.Equal(t, "Ann", second.Name, "served from Redis, not the database")
}

func TestInvalidateCache_MakesTheNextReadFresh(t *testing.T) {
	e := newCacheEnv(t)
	u := e.create(t, "ann@example.com")
	ctx := context.Background()
	_, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
	require.NoError(t, err)
	e.updateBehindTheCacheBack(t, u.ID, "UPDATE users SET name = 'Changed', status = 'suspended' WHERE id = $1")

	require.NoError(t, e.svc.InvalidateCache(ctx, u.ID))

	got, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
	require.NoError(t, err)
	assert.Equal(t, "Changed", got.Name)
	assert.Equal(t, user.StatusSuspended, got.Status, "a suspension is visible immediately after invalidation")
}

func TestInvalidateCache_HandlesSeveralUsersAndNoneAndUnknown(t *testing.T) {
	e := newCacheEnv(t)
	a, b := e.create(t, "a@example.com"), e.create(t, "b@example.com")
	ctx := context.Background()
	for _, u := range []user.User{a, b} {
		_, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
		require.NoError(t, err)
	}

	require.NoError(t, e.svc.InvalidateCache(ctx, a.ID, b.ID, uuid.New()))
	require.NoError(t, e.svc.InvalidateCache(ctx), "no ids is a no-op")

	_, aCached := e.cachedRaw(context.Background(), t, a.ID)
	_, bCached := e.cachedRaw(context.Background(), t, b.ID)
	assert.False(t, aCached)
	assert.False(t, bCached)
}

func TestMarkEmailVerified_InvalidatesTheCacheItself(t *testing.T) {
	e := newCacheEnv(t)
	u := e.create(t, "ann@example.com")
	ctx := context.Background()
	before, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
	require.NoError(t, err)
	require.False(t, before.EmailVerified())

	require.NoError(t, e.svc.MarkEmailVerified(ctx, e.db.Pool(), u.ID))

	after, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
	require.NoError(t, err)
	assert.True(t, after.EmailVerified(), "the write path invalidates; callers need not remember")
}

func TestCache_NeverContainsThePasswordHash(t *testing.T) {
	e := newCacheEnv(t)
	u := e.create(t, "ann@example.com")
	_, err := e.svc.GetSummary(context.Background(), e.db.Pool(), u.ID)
	require.NoError(t, err)

	raw, found := e.cachedRaw(context.Background(), t, u.ID)

	require.True(t, found)
	assert.NotContains(t, string(raw), "secret-hash")
	assert.NotContains(t, string(raw), "password")
	var fields map[string]any
	require.NoError(t, json.Unmarshal(raw, &fields))
	assert.ElementsMatch(t, []string{"id", "email", "name", "status"}, keys(fields), "only the fields authentication needs")
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestCache_EntriesExpireOnTheirOwn(t *testing.T) {
	e := newCacheEnv(t)
	u := e.create(t, "ann@example.com")
	_, err := e.svc.GetSummary(context.Background(), e.db.Pool(), u.ID)
	require.NoError(t, err)

	ttl, err := e.redis.Raw().PTTL(context.Background(), e.redis.Namespace("user").Key("summary:v1:"+u.ID.String())).Result()

	require.NoError(t, err)
	assert.Positive(t, ttl)
	assert.LessOrEqual(t, ttl, cacheTTL, "a missed invalidation can only be stale for one TTL")
}

func TestCache_KeysAreNamespacedPerModuleAndPrefix(t *testing.T) {
	e := newCacheEnv(t)
	u := e.create(t, "ann@example.com")
	_, err := e.svc.GetSummary(context.Background(), e.db.Pool(), u.ID)
	require.NoError(t, err)

	exists, err := e.redis.Raw().Exists(context.Background(), e.redis.Namespace("user").Key("summary:v1:"+u.ID.String())).Result()

	require.NoError(t, err)
	assert.EqualValues(t, 1, exists, "<key_prefix>:user:summary:v1:<id>")
}

func TestGetSummary_UnknownUserIsNotFoundAndNotCached(t *testing.T) {
	e := newCacheEnv(t)
	id := uuid.New()

	_, err := e.svc.GetSummary(context.Background(), e.db.Pool(), id)

	assert.True(t, apperr.IsCode(err, apperr.CodeNotFound))
	_, cached := e.cachedRaw(context.Background(), t, id)
	assert.False(t, cached, "negative results are never cached")
}

func TestGetSummary_SoftDeletedUserIsNotFoundOnceInvalidated(t *testing.T) {
	e := newCacheEnv(t)
	u := e.create(t, "ann@example.com")
	ctx := context.Background()
	_, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
	require.NoError(t, err)
	e.updateBehindTheCacheBack(t, u.ID, "UPDATE users SET deleted_at = now(), status = 'deleted' WHERE id = $1")
	require.NoError(t, e.svc.InvalidateCache(ctx, u.ID))

	_, err = e.svc.GetSummary(ctx, e.db.Pool(), u.ID)

	assert.True(t, apperr.IsCode(err, apperr.CodeNotFound))
}

func TestGetSummary_FallsBackToTheDatabaseWhenRedisIsDown(t *testing.T) {
	e := newCacheEnv(t)
	u := e.create(t, "ann@example.com")
	require.NoError(t, e.redis.Close())

	got, err := e.svc.GetSummary(context.Background(), e.db.Pool(), u.ID)

	require.NoError(t, err, "a cache outage must never take authentication down")
	assert.Equal(t, u.ID, got.ID)
}

func TestInvalidateCache_ReportsRedisFailures(t *testing.T) {
	e := newCacheEnv(t)
	require.NoError(t, e.redis.Close())

	err := e.svc.InvalidateCache(context.Background(), uuid.New())

	assert.Error(t, err, "callers need to know when an invalidation did not happen")
}

func TestGetSummary_CorruptCacheEntryIsReplaced(t *testing.T) {
	e := newCacheEnv(t)
	u := e.create(t, "ann@example.com")
	ctx := context.Background()
	require.NoError(t, e.redis.Namespace("user").Set(ctx, "summary:v1:"+u.ID.String(), []byte("{not json"), time.Minute))

	got, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)

	require.NoError(t, err)
	assert.Equal(t, "Ann", got.Name)
	raw, _ := e.cachedRaw(context.Background(), t, u.ID)
	assert.JSONEq(t, `{"id":"`+u.ID.String()+`","email":"ann@example.com","name":"Ann","status":"active"}`, string(raw), "repaired from the database")
}

func TestGetSummary_EntryOfAnotherUserIsNotTrusted(t *testing.T) {
	e := newCacheEnv(t)
	victim, other := e.create(t, "victim@example.com"), e.create(t, "other@example.com")
	ctx := context.Background()
	poisoned, err := json.Marshal(other.Summary())
	require.NoError(t, err)
	require.NoError(t, e.redis.Namespace("user").Set(ctx, "summary:v1:"+victim.ID.String(), poisoned, time.Minute))

	got, err := e.svc.GetSummary(ctx, e.db.Pool(), victim.ID)

	require.NoError(t, err)
	assert.Equal(t, victim.ID, got.ID, "a mismatched entry is discarded, never returned")
	assert.Equal(t, "victim@example.com", got.Email)
}

func TestGetSummary_WorksWithoutAnyCache(t *testing.T) {
	e := newEnv(t) // service built without WithCache
	u := e.create(t, "ann@example.com")

	got, err := e.svc.GetSummary(context.Background(), e.db.Pool(), u.ID)

	require.NoError(t, err)
	assert.Equal(t, u.ID, got.ID)
	assert.NoError(t, e.svc.InvalidateCache(context.Background(), u.ID), "invalidating without a cache is a no-op")
}

func TestWriteInsideATransaction_InvalidatesOnlyAfterCommit(t *testing.T) {
	e := newCacheEnv(t)
	u := e.create(t, "ann@example.com")
	ctx := context.Background()
	_, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
	require.NoError(t, err)

	err = e.db.WithTx(ctx, func(tx pgx.Tx) error {
		if err := e.svc.MarkEmailVerified(ctx, tx, u.ID); err != nil {
			return err
		}
		_, stillCached := e.cachedRaw(ctx, t, u.ID)
		assert.True(t, stillCached, "nothing is dropped before the commit")
		return nil
	})

	require.NoError(t, err)
	_, cached := e.cachedRaw(context.Background(), t, u.ID)
	assert.False(t, cached, "dropped right after the commit, with no extra call from the caller")
}

func TestWriteInsideATransaction_ARolledBackWriteLeavesTheCacheAlone(t *testing.T) {
	e := newCacheEnv(t)
	u := e.create(t, "ann@example.com")
	ctx := context.Background()
	_, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
	require.NoError(t, err)

	err = e.db.WithTx(ctx, func(tx pgx.Tx) error {
		if err := e.svc.MarkEmailVerified(ctx, tx, u.ID); err != nil {
			return err
		}
		return assert.AnError
	})

	require.Error(t, err)
	_, cached := e.cachedRaw(context.Background(), t, u.ID)
	assert.True(t, cached, "the row did not change, so the entry is still correct")
	got, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
	require.NoError(t, err)
	assert.False(t, got.EmailVerified())
}

// This is the race that invalidating BEFORE the commit would lose: a reader
// re-caches the old row while the writer's transaction is still open.
func TestWriteInsideATransaction_AReaderCannotLeaveAStaleEntryBehind(t *testing.T) {
	e := newCacheEnv(t)
	u := e.create(t, "ann@example.com")
	ctx := context.Background()

	err := e.db.WithTx(ctx, func(tx pgx.Tx) error {
		if err := e.svc.MarkEmailVerified(ctx, tx, u.ID); err != nil {
			return err
		}
		// A concurrent request misses the cache and reads the not-yet-committed
		// row from the database, caching the OLD (unverified) state.
		stale, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
		require.NoError(t, err)
		require.False(t, stale.EmailVerified())
		_, cached := e.cachedRaw(ctx, t, u.ID)
		require.True(t, cached, "the stale entry exists while the writer is still open")
		return nil
	})
	require.NoError(t, err)

	got, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)

	require.NoError(t, err)
	assert.True(t, got.EmailVerified(), "the post-commit invalidation wiped the stale entry")
}

func TestWriteOutsideATransaction_InvalidatesImmediately(t *testing.T) {
	e := newCacheEnv(t)
	u := e.create(t, "ann@example.com")
	ctx := context.Background()
	_, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
	require.NoError(t, err)

	require.NoError(t, e.svc.MarkEmailVerified(ctx, e.db.Pool(), u.ID))

	_, cached := e.cachedRaw(context.Background(), t, u.ID)
	assert.False(t, cached)
}

func TestSetPasswordHash_LeavesTheSummaryCacheAlone(t *testing.T) {
	e := newCacheEnv(t)
	u := e.create(t, "ann@example.com")
	ctx := context.Background()
	_, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
	require.NoError(t, err)

	require.NoError(t, e.svc.SetPasswordHash(ctx, e.db.Pool(), u.ID, "new-hash"))

	_, cached := e.cachedRaw(context.Background(), t, u.ID)
	assert.True(t, cached, "the summary has no password data, so nothing is stale")
}
