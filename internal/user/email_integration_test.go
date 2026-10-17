//go:build integration

package user_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/apperr"
)

func TestChangeEmail_StoresTheNormalizedAddressAsVerified(t *testing.T) {
	e := newEnv(t)
	u := e.create(t, "ann@example.com")
	require.False(t, u.EmailVerified())
	e.clock.Advance(time.Hour)

	require.NoError(t, e.svc.ChangeEmail(context.Background(), e.db.Pool(), u.ID, "  New@Example.COM "))

	got, err := e.svc.GetByID(context.Background(), e.db.Pool(), u.ID)
	require.NoError(t, err)
	assert.Equal(t, "new@example.com", got.Email)
	require.True(t, got.EmailVerified(), "the link sent to the new address proved ownership")
	assert.True(t, got.EmailVerifiedAt.Equal(e.clock.Now()))
	assert.True(t, got.UpdatedAt.After(u.UpdatedAt))
	_, err = e.svc.GetByEmail(context.Background(), e.db.Pool(), "ann@example.com")
	assert.True(t, apperr.IsCode(err, apperr.CodeNotFound), "the old address is free again")
}

func TestChangeEmail_TakenAddressIsAConflictRegardlessOfCase(t *testing.T) {
	e := newEnv(t)
	ann := e.create(t, "ann@example.com")
	e.create(t, "bob@example.com")

	err := e.svc.ChangeEmail(context.Background(), e.db.Pool(), ann.ID, "BOB@example.com")

	assert.True(t, apperr.IsCode(err, apperr.CodeConflict), "got %v", err)
	got, err := e.svc.GetByID(context.Background(), e.db.Pool(), ann.ID)
	require.NoError(t, err)
	assert.Equal(t, "ann@example.com", got.Email)
}

func TestChangeEmail_UnknownOrDeletedUserIsNotFound(t *testing.T) {
	e := newEnv(t)
	gone := e.create(t, "gone@example.com")
	_, err := e.db.Pool().Exec(context.Background(), "UPDATE users SET deleted_at = now(), status = 'deleted' WHERE id = $1", gone.ID)
	require.NoError(t, err)

	err = e.svc.ChangeEmail(context.Background(), e.db.Pool(), gone.ID, "new@example.com")

	assert.True(t, apperr.IsCode(err, apperr.CodeNotFound), "got %v", err)
}

func TestChangeEmail_InvalidatesTheCacheItself(t *testing.T) {
	e := newCacheEnv(t)
	u := e.create(t, "ann@example.com")
	ctx := context.Background()
	before, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
	require.NoError(t, err)
	require.Equal(t, "ann@example.com", before.Email)

	require.NoError(t, e.svc.ChangeEmail(ctx, e.db.Pool(), u.ID, "new@example.com"))

	after, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
	require.NoError(t, err)
	assert.Equal(t, "new@example.com", after.Email, "never stale")
	assert.True(t, after.EmailVerified())
}

func TestChangeEmail_InsideATransactionInvalidatesOnlyAfterCommitAndNotOnRollback(t *testing.T) {
	e := newCacheEnv(t)
	u := e.create(t, "ann@example.com")
	ctx := context.Background()
	_, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
	require.NoError(t, err)

	_ = e.db.WithTx(ctx, func(tx pgx.Tx) error {
		require.NoError(t, e.svc.ChangeEmail(ctx, tx, u.ID, "rolled-back@example.com"))
		return assert.AnError
	})
	got, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
	require.NoError(t, err)
	assert.Equal(t, "ann@example.com", got.Email, "a rolled-back change leaves a correct cache")

	err = e.db.WithTx(ctx, func(tx pgx.Tx) error {
		if err := e.svc.ChangeEmail(ctx, tx, u.ID, "new@example.com"); err != nil {
			return err
		}
		_, stillCached := e.cachedRaw(ctx, t, u.ID)
		assert.True(t, stillCached, "nothing is dropped before the commit")
		return nil
	})
	require.NoError(t, err)
	_, cached := e.cachedRaw(ctx, t, u.ID)
	assert.False(t, cached)
}
