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
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/dbtest"
	"github.com/kangsolder001/saka/internal/user"
)

var epoch = time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC)

type env struct {
	db    *db.DB
	clock *clock.Fake
	svc   *user.Service
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
	return &env{db: d, clock: clk, svc: user.NewService(clk)}
}

func (e *env) create(t *testing.T, email string) user.User {
	t.Helper()
	u, err := e.svc.Create(context.Background(), e.db.Pool(), user.NewUser{Email: email, PasswordHash: "hash", Name: "Ann"})
	require.NoError(t, err)
	return u
}

func TestCreate_StoresNormalizedUserWithDefaults(t *testing.T) {
	e := newEnv(t)

	u, err := e.svc.Create(context.Background(), e.db.Pool(), user.NewUser{
		Email: "  Ann@Example.COM ", PasswordHash: "hash", Name: "  Ann  ",
	})

	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, u.ID)
	assert.Equal(t, "ann@example.com", u.Email)
	assert.Equal(t, "Ann", u.Name)
	assert.Equal(t, "hash", u.PasswordHash)
	assert.Equal(t, user.StatusActive, u.Status)
	assert.False(t, u.EmailVerified())
	assert.True(t, epoch.Equal(u.CreatedAt), "timestamps come from the injected clock")
	assert.True(t, epoch.Equal(u.UpdatedAt))
}

func TestCreate_DuplicateEmailIsConflictRegardlessOfCase(t *testing.T) {
	e := newEnv(t)
	e.create(t, "ann@example.com")

	_, err := e.svc.Create(context.Background(), e.db.Pool(), user.NewUser{Email: "ANN@example.com", PasswordHash: "h", Name: "Other"})

	assert.True(t, apperr.IsCode(err, apperr.CodeConflict))
}

func TestCreate_InsideRolledBackTransactionLeavesNothing(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	err := e.db.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := e.svc.Create(ctx, tx, user.NewUser{Email: "tx@example.com", PasswordHash: "h", Name: "Tx"}); err != nil {
			return err
		}
		return assert.AnError
	})

	require.Error(t, err)
	_, err = e.svc.GetByEmail(ctx, e.db.Pool(), "tx@example.com")
	assert.True(t, apperr.IsCode(err, apperr.CodeNotFound))
}

func TestGetByIDAndEmail(t *testing.T) {
	e := newEnv(t)
	created := e.create(t, "ann@example.com")
	ctx := context.Background()

	byID, err := e.svc.GetByID(ctx, e.db.Pool(), created.ID)
	require.NoError(t, err)
	byEmail, err := e.svc.GetByEmail(ctx, e.db.Pool(), "  ANN@example.com")
	require.NoError(t, err)

	assert.Equal(t, created.ID, byID.ID)
	assert.Equal(t, created.ID, byEmail.ID)
}

func TestGet_UnknownIsNotFound(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	_, err := e.svc.GetByID(ctx, e.db.Pool(), uuid.New())
	assert.True(t, apperr.IsCode(err, apperr.CodeNotFound))
	_, err = e.svc.GetByEmail(ctx, e.db.Pool(), "nobody@example.com")
	assert.True(t, apperr.IsCode(err, apperr.CodeNotFound))
}

func TestGet_SoftDeletedUserIsNotFound(t *testing.T) {
	e := newEnv(t)
	u := e.create(t, "gone@example.com")
	_, err := e.db.Pool().Exec(context.Background(),
		"UPDATE users SET deleted_at = now(), status = 'deleted' WHERE id = $1", u.ID)
	require.NoError(t, err)

	_, err = e.svc.GetByID(context.Background(), e.db.Pool(), u.ID)
	assert.True(t, apperr.IsCode(err, apperr.CodeNotFound))
	_, err = e.svc.GetByEmail(context.Background(), e.db.Pool(), "gone@example.com")
	assert.True(t, apperr.IsCode(err, apperr.CodeNotFound))
}

func TestMarkEmailVerified_IsIdempotent(t *testing.T) {
	e := newEnv(t)
	u := e.create(t, "ann@example.com")
	ctx := context.Background()

	require.NoError(t, e.svc.MarkEmailVerified(ctx, e.db.Pool(), u.ID))
	firstAt := e.clock.Now()
	e.clock.Advance(time.Hour)
	require.NoError(t, e.svc.MarkEmailVerified(ctx, e.db.Pool(), u.ID))

	got, err := e.svc.GetByID(ctx, e.db.Pool(), u.ID)
	require.NoError(t, err)
	assert.True(t, got.EmailVerified())
	assert.True(t, firstAt.Equal(*got.EmailVerifiedAt), "the first verification time is kept")
}

func TestSetPasswordHash(t *testing.T) {
	e := newEnv(t)
	u := e.create(t, "ann@example.com")
	ctx := context.Background()
	e.clock.Advance(time.Minute)

	require.NoError(t, e.svc.SetPasswordHash(ctx, e.db.Pool(), u.ID, "new-hash"))

	got, err := e.svc.GetByID(ctx, e.db.Pool(), u.ID)
	require.NoError(t, err)
	assert.Equal(t, "new-hash", got.PasswordHash)
	assert.True(t, e.clock.Now().Equal(got.UpdatedAt))

	err = e.svc.SetPasswordHash(ctx, e.db.Pool(), uuid.New(), "x")
	assert.True(t, apperr.IsCode(err, apperr.CodeNotFound))
}

func TestGetSummaries_ReturnsKnownActiveUsersByID(t *testing.T) {
	e := newEnv(t)
	ann, bob := e.create(t, "ann@example.com"), e.create(t, "bob@example.com")
	gone := e.create(t, "gone@example.com")
	require.NoError(t, e.svc.Delete(context.Background(), e.db.Pool(), gone.ID))

	got, err := e.svc.GetSummaries(context.Background(), e.db.Pool(), []uuid.UUID{ann.ID, bob.ID, gone.ID, uuid.New()})

	require.NoError(t, err)
	assert.Len(t, got, 2, "unknown and deleted users are absent")
	assert.Equal(t, "ann@example.com", got[ann.ID].Email)
	assert.Equal(t, "bob@example.com", got[bob.ID].Email)
}

func TestGetSummaries_EmptyInputNeedsNoQuery(t *testing.T) {
	e := newEnv(t)

	got, err := e.svc.GetSummaries(context.Background(), e.db.Pool(), nil)

	require.NoError(t, err)
	assert.Empty(t, got)
}
