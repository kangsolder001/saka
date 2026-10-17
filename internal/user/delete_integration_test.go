//go:build integration

package user_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/user"
)

func TestDelete_SoftDeletesAndAnonymizesTheEmail(t *testing.T) {
	e := newEnv(t)
	u := e.create(t, "ann@example.com")
	e.clock.Advance(time.Hour)

	require.NoError(t, e.svc.Delete(context.Background(), e.db.Pool(), u.ID))

	var email, status string
	var deletedAt *time.Time
	require.NoError(t, e.db.Pool().QueryRow(context.Background(),
		"SELECT email, status, deleted_at FROM users WHERE id = $1", u.ID).Scan(&email, &status, &deletedAt))
	assert.Equal(t, "deleted+"+u.ID.String()+"@invalid", email)
	assert.Equal(t, string(user.StatusDeleted), status)
	require.NotNil(t, deletedAt, "the row stays: this is a soft delete")
	assert.True(t, deletedAt.Equal(e.clock.Now()))
	_, err := e.svc.GetByID(context.Background(), e.db.Pool(), u.ID)
	assert.True(t, apperr.IsCode(err, apperr.CodeNotFound), "deleted users count as missing")
	_, err = e.svc.GetByEmail(context.Background(), e.db.Pool(), "ann@example.com")
	assert.True(t, apperr.IsCode(err, apperr.CodeNotFound))
}

func TestDelete_FreesTheEmailForANewSignUp(t *testing.T) {
	e := newEnv(t)
	old := e.create(t, "ann@example.com")
	require.NoError(t, e.svc.Delete(context.Background(), e.db.Pool(), old.ID))

	again, err := e.svc.Create(context.Background(), e.db.Pool(), user.NewUser{Email: "Ann@Example.com", PasswordHash: "hash", Name: "Ann"})

	require.NoError(t, err, "the real address can sign up again")
	assert.NotEqual(t, old.ID, again.ID)
}

func TestDelete_TwoDeletedAccountsDoNotCollide(t *testing.T) {
	e := newEnv(t)
	a, b := e.create(t, "a@example.com"), e.create(t, "b@example.com")

	require.NoError(t, e.svc.Delete(context.Background(), e.db.Pool(), a.ID))
	require.NoError(t, e.svc.Delete(context.Background(), e.db.Pool(), b.ID), "each placeholder email carries its own ID")
}

func TestDelete_UnknownOrAlreadyDeletedIsNotFound(t *testing.T) {
	e := newEnv(t)
	u := e.create(t, "ann@example.com")
	require.NoError(t, e.svc.Delete(context.Background(), e.db.Pool(), u.ID))

	for _, id := range []uuid.UUID{uuid.New(), u.ID} {
		err := e.svc.Delete(context.Background(), e.db.Pool(), id)

		assert.True(t, apperr.IsCode(err, apperr.CodeNotFound), "got %v", err)
	}
}

func TestDelete_InvalidatesTheCacheSoTheAccountStopsWorkingAtOnce(t *testing.T) {
	e := newCacheEnv(t)
	u := e.create(t, "ann@example.com")
	ctx := context.Background()
	before, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
	require.NoError(t, err)
	require.Equal(t, user.StatusActive, before.Status)

	require.NoError(t, e.svc.Delete(ctx, e.db.Pool(), u.ID))

	_, err = e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
	assert.True(t, apperr.IsCode(err, apperr.CodeNotFound), "never served from a stale 'active' entry; got %v", err)
}

func TestDelete_InsideATransactionInvalidatesOnlyAfterCommitAndNotOnRollback(t *testing.T) {
	e := newCacheEnv(t)
	u := e.create(t, "ann@example.com")
	ctx := context.Background()
	_, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
	require.NoError(t, err)

	_ = e.db.WithTx(ctx, func(tx pgx.Tx) error {
		require.NoError(t, e.svc.Delete(ctx, tx, u.ID))
		return assert.AnError
	})
	got, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
	require.NoError(t, err, "a rolled-back deletion leaves the user and a correct cache")
	assert.Equal(t, user.StatusActive, got.Status)

	err = e.db.WithTx(ctx, func(tx pgx.Tx) error {
		if err := e.svc.Delete(ctx, tx, u.ID); err != nil {
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
