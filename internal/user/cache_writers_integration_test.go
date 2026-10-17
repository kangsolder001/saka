//go:build integration

package user_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/user"
)

// writeCalls runs one write method of Service for a user. Every name in
// cacheWriters needs an entry, so a new write method cannot skip these tests.
var writeCalls = map[string]func(ctx context.Context, svc *user.Service, q db.DBTX, id uuid.UUID) error{
	"MarkEmailVerified": func(ctx context.Context, svc *user.Service, q db.DBTX, id uuid.UUID) error {
		return svc.MarkEmailVerified(ctx, q, id)
	},
	"UpdateProfile": func(ctx context.Context, svc *user.Service, q db.DBTX, id uuid.UUID) error {
		_, err := svc.UpdateProfile(ctx, q, id, user.ProfileUpdate{Name: ptr("Ann Lee")})
		return err
	},
	"ChangeEmail": func(ctx context.Context, svc *user.Service, q db.DBTX, id uuid.UUID) error {
		return svc.ChangeEmail(ctx, q, id, "new@example.com")
	},
	"Delete": func(ctx context.Context, svc *user.Service, q db.DBTX, id uuid.UUID) error {
		return svc.Delete(ctx, q, id)
	},
}

func TestCacheWriters_EveryWriteMethodHasATestCase(t *testing.T) {
	for _, name := range cacheWriters {
		assert.Contains(t, writeCalls, name, "add %s to writeCalls so its cache invalidation is proven", name)
	}
	assert.Len(t, writeCalls, len(cacheWriters), "writeCalls has an entry that is not listed in cacheWriters")
}

// primed returns an environment where the user's summary is in the cache.
func primed(t *testing.T) (*cacheEnv, user.User) {
	t.Helper()
	e := newCacheEnv(t)
	u := e.create(t, "ann@example.com")
	_, err := e.svc.GetSummary(context.Background(), e.db.Pool(), u.ID)
	require.NoError(t, err)
	_, cached := e.cachedRaw(context.Background(), t, u.ID)
	require.True(t, cached, "precondition: the summary is cached")
	return e, u
}

func TestCacheWriters_WithoutATransactionTheCacheIsDroppedRightAway(t *testing.T) {
	for name, call := range writeCalls {
		t.Run(name, func(t *testing.T) {
			e, u := primed(t)

			require.NoError(t, call(context.Background(), e.svc, e.db.Pool(), u.ID))

			_, cached := e.cachedRaw(context.Background(), t, u.ID)
			assert.False(t, cached, "each statement on the pool commits on its own, so the entry goes at once")
		})
	}
}

func TestCacheWriters_InATransactionTheCacheIsDroppedOnlyAfterTheCommit(t *testing.T) {
	for name, call := range writeCalls {
		t.Run(name, func(t *testing.T) {
			e, u := primed(t)
			ctx := context.Background()

			err := e.db.WithTx(ctx, func(tx pgx.Tx) error {
				if err := call(ctx, e.svc, tx, u.ID); err != nil {
					return err
				}
				// Dropping it here would let a concurrent reader re-cache the old row.
				_, stillCached := e.cachedRaw(ctx, t, u.ID)
				assert.True(t, stillCached, "nothing is dropped before the commit")
				return nil
			})

			require.NoError(t, err)
			_, cached := e.cachedRaw(ctx, t, u.ID)
			assert.False(t, cached, "dropped right after the commit, with no extra call from the caller")
		})
	}
}

func TestCacheWriters_ARolledBackWriteLeavesTheCachedValueCorrect(t *testing.T) {
	for name, call := range writeCalls {
		t.Run(name, func(t *testing.T) {
			e, u := primed(t)
			ctx := context.Background()
			before, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
			require.NoError(t, err)

			_ = e.db.WithTx(ctx, func(tx pgx.Tx) error {
				require.NoError(t, call(ctx, e.svc, tx, u.ID))
				return assert.AnError
			})

			after, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
			require.NoError(t, err, "the change never happened, so the user is still readable")
			assert.Equal(t, before, after, "and the summary, cached or not, still matches the database")
		})
	}
}

func TestCacheWriters_TheNextReadAlwaysSeesTheWrite(t *testing.T) {
	// A read after a committed write must come from the database, never from a
	// stale entry: this is the user-visible meaning of "never stale".
	e, u := primed(t)
	ctx := context.Background()
	require.NoError(t, e.svc.ChangeEmail(ctx, e.db.Pool(), u.ID, "new@example.com"))
	afterEmail, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
	require.NoError(t, err)
	assert.Equal(t, "new@example.com", afterEmail.Email)

	_, err = e.svc.UpdateProfile(ctx, e.db.Pool(), u.ID, user.ProfileUpdate{Name: ptr("Ann Lee")})
	require.NoError(t, err)
	afterProfile, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
	require.NoError(t, err)
	assert.Equal(t, "Ann Lee", afterProfile.Name)
	assert.Equal(t, "new@example.com", afterProfile.Email)

	require.NoError(t, e.svc.Delete(ctx, e.db.Pool(), u.ID))
	_, err = e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
	assert.Error(t, err, "a deleted user is not served from the cache")
}
