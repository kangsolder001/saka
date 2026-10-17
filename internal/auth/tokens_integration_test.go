//go:build integration

package auth_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/auth"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/dbtest"
	"github.com/kangsolder001/saka/internal/user"
)

var epoch = time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC)

type env struct {
	db     *db.DB
	clock  *clock.Fake
	users  *user.Service
	tokens *auth.UserTokens
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
	return &env{db: d, clock: clk, users: user.NewService(clk), tokens: auth.NewUserTokens(clk)}
}

func (e *env) newUser(t *testing.T) user.User {
	t.Helper()
	u, err := e.users.Create(context.Background(), e.db.Pool(),
		user.NewUser{Email: uuid.NewString() + "@example.com", PasswordHash: "h", Name: "Ann"})
	require.NoError(t, err)
	return u
}

func (e *env) issue(t *testing.T, u user.User, typ auth.TokenType, payload any) string {
	t.Helper()
	raw, err := e.tokens.Issue(context.Background(), e.db.Pool(), u.ID, typ, payload)
	require.NoError(t, err)
	return raw
}

func isInvalidToken(err error) bool { return apperr.IsCode(err, apperr.CodeValidation) }

func TestUserTokens_IssueStoresOnlyTheHash(t *testing.T) {
	e := newEnv(t)
	u := e.newUser(t)

	raw := e.issue(t, u, auth.TokenEmailVerification, nil)

	var hash string
	var expires time.Time
	require.NoError(t, e.db.Pool().QueryRow(context.Background(),
		"SELECT token_hash, expires_at FROM user_tokens WHERE user_id = $1", u.ID).Scan(&hash, &expires))
	assert.Equal(t, auth.HashToken(raw), hash)
	assert.NotEqual(t, raw, hash)
	assert.True(t, epoch.Add(24*time.Hour).Equal(expires))
}

func TestUserTokens_ConsumeWorksOnceOnly(t *testing.T) {
	e := newEnv(t)
	u := e.newUser(t)
	raw := e.issue(t, u, auth.TokenEmailVerification, nil)
	ctx := context.Background()

	got, err := e.tokens.Consume(ctx, e.db.Pool(), raw, auth.TokenEmailVerification)
	require.NoError(t, err)
	assert.Equal(t, u.ID, got.UserID)
	assert.Equal(t, auth.TokenEmailVerification, got.Type)

	_, err = e.tokens.Consume(ctx, e.db.Pool(), raw, auth.TokenEmailVerification)
	assert.True(t, isInvalidToken(err), "a used token is rejected")

	var usedAt *time.Time
	require.NoError(t, e.db.Pool().QueryRow(ctx, "SELECT used_at FROM user_tokens WHERE user_id = $1", u.ID).Scan(&usedAt))
	require.NotNil(t, usedAt)
	assert.True(t, epoch.Equal(*usedAt))
}

func TestUserTokens_ExpiryFollowsTypeAndClock(t *testing.T) {
	tests := []struct {
		typ      auth.TokenType
		ttl      time.Duration
		justOver time.Duration
	}{
		{auth.TokenPasswordReset, time.Hour, time.Second},
		{auth.TokenEmailVerification, 24 * time.Hour, time.Second},
		{auth.TokenEmailChange, 24 * time.Hour, time.Second},
	}
	for _, tt := range tests {
		t.Run(string(tt.typ), func(t *testing.T) {
			e := newEnv(t)
			u := e.newUser(t)
			ctx := context.Background()

			valid := e.issue(t, u, tt.typ, nil)
			e.clock.Advance(tt.ttl - time.Second)
			_, err := e.tokens.Consume(ctx, e.db.Pool(), valid, tt.typ)
			require.NoError(t, err, "still valid one second before expiry")

			expired := e.issue(t, u, tt.typ, nil)
			e.clock.Advance(tt.ttl + tt.justOver)
			_, err = e.tokens.Consume(ctx, e.db.Pool(), expired, tt.typ)
			assert.True(t, isInvalidToken(err), "expired token is rejected")
		})
	}
}

func TestUserTokens_WrongTypeOrUnknownTokenIsRejected(t *testing.T) {
	e := newEnv(t)
	u := e.newUser(t)
	raw := e.issue(t, u, auth.TokenEmailVerification, nil)
	ctx := context.Background()

	_, err := e.tokens.Consume(ctx, e.db.Pool(), raw, auth.TokenPasswordReset)
	assert.True(t, isInvalidToken(err), "a verification token cannot reset a password")
	_, err = e.tokens.Consume(ctx, e.db.Pool(), "not-a-real-token", auth.TokenEmailVerification)
	assert.True(t, isInvalidToken(err))
	_, err = e.tokens.Consume(ctx, e.db.Pool(), "", auth.TokenEmailVerification)
	assert.True(t, isInvalidToken(err))

	_, err = e.tokens.Consume(ctx, e.db.Pool(), raw, auth.TokenEmailVerification)
	assert.NoError(t, err, "the failed attempts above did not burn the token")
}

func TestUserTokens_NewTokenInvalidatesOlderUnusedOnes(t *testing.T) {
	e := newEnv(t)
	u := e.newUser(t)
	other := e.newUser(t)
	old := e.issue(t, u, auth.TokenPasswordReset, nil)
	othersToken := e.issue(t, other, auth.TokenPasswordReset, nil)
	differentType := e.issue(t, u, auth.TokenEmailVerification, nil)

	newer := e.issue(t, u, auth.TokenPasswordReset, nil)

	ctx := context.Background()
	_, err := e.tokens.Consume(ctx, e.db.Pool(), old, auth.TokenPasswordReset)
	assert.True(t, isInvalidToken(err), "the older link stops working")
	_, err = e.tokens.Consume(ctx, e.db.Pool(), newer, auth.TokenPasswordReset)
	assert.NoError(t, err)
	_, err = e.tokens.Consume(ctx, e.db.Pool(), othersToken, auth.TokenPasswordReset)
	assert.NoError(t, err, "other users are unaffected")
	_, err = e.tokens.Consume(ctx, e.db.Pool(), differentType, auth.TokenEmailVerification)
	assert.NoError(t, err, "other token types are unaffected")
}

func TestUserTokens_PayloadRoundTrips(t *testing.T) {
	e := newEnv(t)
	u := e.newUser(t)
	raw := e.issue(t, u, auth.TokenEmailChange, map[string]string{"new_email": "new@example.com"})

	got, err := e.tokens.Consume(context.Background(), e.db.Pool(), raw, auth.TokenEmailChange)

	require.NoError(t, err)
	assert.JSONEq(t, `{"new_email":"new@example.com"}`, string(got.Payload))
}

func TestUserTokens_ConcurrentConsumeHasExactlyOneWinner(t *testing.T) {
	e := newEnv(t)
	u := e.newUser(t)
	raw := e.issue(t, u, auth.TokenPasswordReset, nil)

	var wins atomic.Int32
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.tokens.Consume(context.Background(), e.db.Pool(), raw, auth.TokenPasswordReset); err == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()

	assert.EqualValues(t, 1, wins.Load(), "a single-use token must work exactly once even under a race")
}

func TestUserTokens_IssueInRolledBackTransactionLeavesNoToken(t *testing.T) {
	e := newEnv(t)
	u := e.newUser(t)
	ctx := context.Background()
	var raw string

	err := e.db.WithTx(ctx, func(tx pgx.Tx) error {
		var err error
		raw, err = e.tokens.Issue(ctx, tx, u.ID, auth.TokenEmailVerification, nil)
		require.NoError(t, err)
		return assert.AnError
	})

	require.Error(t, err)
	_, err = e.tokens.Consume(ctx, e.db.Pool(), raw, auth.TokenEmailVerification)
	assert.True(t, isInvalidToken(err))
}
