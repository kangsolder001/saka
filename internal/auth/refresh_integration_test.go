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
)

const refreshTTL = 30 * 24 * time.Hour

func TestRefreshTokens_StartSessionStoresOnlyTheHash(t *testing.T) {
	e := newEnv(t)
	u := e.newUser(t)
	store := auth.NewRefreshTokens(e.clock, refreshTTL)

	tok, err := store.StartSession(context.Background(), e.db.Pool(), u.ID, auth.ClientInfo{UserAgent: "UA", IP: "203.0.113.5"})

	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, tok.FamilyID)
	assert.True(t, epoch.Add(refreshTTL).Equal(tok.ExpiresAt))
	var hash, ua, ip string
	var family uuid.UUID
	require.NoError(t, e.db.Pool().QueryRow(context.Background(),
		"SELECT token_hash, user_agent, ip, family_id FROM refresh_tokens WHERE user_id = $1", u.ID).Scan(&hash, &ua, &ip, &family))
	assert.Equal(t, auth.HashToken(tok.Raw), hash)
	assert.Equal(t, "UA", ua)
	assert.Equal(t, "203.0.113.5", ip)
	assert.Equal(t, tok.FamilyID, family)
}

func TestRefreshTokens_TruncatesHugeUserAgents(t *testing.T) {
	e := newEnv(t)
	u := e.newUser(t)
	store := auth.NewRefreshTokens(e.clock, refreshTTL)
	huge := make([]byte, 5000)
	for i := range huge {
		huge[i] = 'x'
	}

	_, err := store.StartSession(context.Background(), e.db.Pool(), u.ID, auth.ClientInfo{UserAgent: string(huge)})

	require.NoError(t, err)
	var n int
	require.NoError(t, e.db.Pool().QueryRow(context.Background(), "SELECT length(user_agent) FROM refresh_tokens").Scan(&n))
	assert.Equal(t, 512, n)
}

func TestRefreshTokens_SessionActive(t *testing.T) {
	e := newEnv(t)
	u, other := e.newUser(t), e.newUser(t)
	store := auth.NewRefreshTokens(e.clock, refreshTTL)
	ctx := context.Background()
	tok, err := store.StartSession(ctx, e.db.Pool(), u.ID, auth.ClientInfo{})
	require.NoError(t, err)

	active, err := store.SessionActive(ctx, e.db.Pool(), u.ID, tok.FamilyID)
	require.NoError(t, err)
	assert.True(t, active)

	active, err = store.SessionActive(ctx, e.db.Pool(), other.ID, tok.FamilyID)
	require.NoError(t, err)
	assert.False(t, active, "a session id only counts for its own user")

	active, err = store.SessionActive(ctx, e.db.Pool(), u.ID, uuid.New())
	require.NoError(t, err)
	assert.False(t, active, "unknown session")

	e.clock.Advance(refreshTTL + time.Second)
	active, err = store.SessionActive(ctx, e.db.Pool(), u.ID, tok.FamilyID)
	require.NoError(t, err)
	assert.False(t, active, "expired sessions are not active")

	_, err = e.db.Pool().Exec(ctx, "UPDATE refresh_tokens SET expires_at = $1, revoked_at = $2", e.clock.Now().Add(time.Hour), e.clock.Now())
	require.NoError(t, err)
	active, err = store.SessionActive(ctx, e.db.Pool(), u.ID, tok.FamilyID)
	require.NoError(t, err)
	assert.False(t, active, "revoked sessions are not active")
}

func (e *env) rotate(t *testing.T, store *auth.RefreshTokens, raw string) auth.RotateResult {
	t.Helper()
	var res auth.RotateResult
	require.NoError(t, e.db.WithTx(context.Background(), func(tx pgx.Tx) error {
		var err error
		res, err = store.Rotate(context.Background(), tx, raw, auth.ClientInfo{UserAgent: "UA2", IP: "198.51.100.7"})
		return err
	}))
	return res
}

func TestRotate_ReplacesTheTokenAndKeepsTheFamily(t *testing.T) {
	e := newEnv(t)
	u := e.newUser(t)
	store := auth.NewRefreshTokens(e.clock, refreshTTL)
	first, err := store.StartSession(context.Background(), e.db.Pool(), u.ID, auth.ClientInfo{})
	require.NoError(t, err)
	e.clock.Advance(time.Hour)

	res := e.rotate(t, store, first.Raw)

	require.Equal(t, auth.Rotated, res.Outcome)
	assert.Equal(t, u.ID, res.UserID)
	assert.Equal(t, first.FamilyID, res.Token.FamilyID, "same login, same family")
	assert.NotEqual(t, first.Raw, res.Token.Raw)
	assert.True(t, e.clock.Now().Add(refreshTTL).Equal(res.Token.ExpiresAt), "the new token gets a full lifetime")

	var revoked *time.Time
	var replacedBy *uuid.UUID
	var newID uuid.UUID
	ctx := context.Background()
	require.NoError(t, e.db.Pool().QueryRow(ctx, "SELECT revoked_at, replaced_by FROM refresh_tokens WHERE token_hash = $1",
		auth.HashToken(first.Raw)).Scan(&revoked, &replacedBy))
	require.NoError(t, e.db.Pool().QueryRow(ctx, "SELECT id FROM refresh_tokens WHERE token_hash = $1", auth.HashToken(res.Token.Raw)).Scan(&newID))
	require.NotNil(t, revoked)
	assert.True(t, e.clock.Now().Equal(*revoked))
	require.NotNil(t, replacedBy)
	assert.Equal(t, newID, *replacedBy, "replaced_by links old token to its successor")

	var ua, ip string
	require.NoError(t, e.db.Pool().QueryRow(ctx, "SELECT user_agent, ip FROM refresh_tokens WHERE id = $1", newID).Scan(&ua, &ip))
	assert.Equal(t, "UA2", ua, "the new token records the client that refreshed")
	assert.Equal(t, "198.51.100.7", ip)

	active, err := store.SessionActive(ctx, e.db.Pool(), u.ID, first.FamilyID)
	require.NoError(t, err)
	assert.True(t, active)
}

func TestRotate_CanChainManyTimes(t *testing.T) {
	e := newEnv(t)
	u := e.newUser(t)
	store := auth.NewRefreshTokens(e.clock, refreshTTL)
	current, err := store.StartSession(context.Background(), e.db.Pool(), u.ID, auth.ClientInfo{})
	require.NoError(t, err)

	for range 5 {
		res := e.rotate(t, store, current.Raw)
		require.Equal(t, auth.Rotated, res.Outcome)
		current = res.Token
	}

	var live, total int
	require.NoError(t, e.db.Pool().QueryRow(context.Background(),
		"SELECT count(*) FILTER (WHERE revoked_at IS NULL), count(*) FROM refresh_tokens").Scan(&live, &total))
	assert.Equal(t, 1, live, "exactly one live token per session")
	assert.Equal(t, 6, total)
}

func TestRotate_UnknownAndExpiredTokens(t *testing.T) {
	e := newEnv(t)
	u := e.newUser(t)
	store := auth.NewRefreshTokens(e.clock, refreshTTL)
	tok, err := store.StartSession(context.Background(), e.db.Pool(), u.ID, auth.ClientInfo{})
	require.NoError(t, err)

	assert.Equal(t, auth.Unknown, e.rotate(t, store, "never-issued").Outcome)
	assert.Equal(t, auth.Unknown, e.rotate(t, store, "").Outcome)

	e.clock.Advance(refreshTTL + time.Second)
	assert.Equal(t, auth.Expired, e.rotate(t, store, tok.Raw).Outcome)
	var revoked int
	require.NoError(t, e.db.Pool().QueryRow(context.Background(), "SELECT count(*) FROM refresh_tokens WHERE revoked_at IS NOT NULL").Scan(&revoked))
	assert.Zero(t, revoked, "an expired token is rejected, not treated as theft")
}

func TestRotate_ReusingAnOldTokenRevokesTheWholeFamilyOnly(t *testing.T) {
	e := newEnv(t)
	u := e.newUser(t)
	store := auth.NewRefreshTokens(e.clock, refreshTTL)
	ctx := context.Background()
	stolen, err := store.StartSession(ctx, e.db.Pool(), u.ID, auth.ClientInfo{})
	require.NoError(t, err)
	otherLogin, err := store.StartSession(ctx, e.db.Pool(), u.ID, auth.ClientInfo{}) // a second device
	require.NoError(t, err)

	legit := e.rotate(t, store, stolen.Raw) // the real user refreshes
	require.Equal(t, auth.Rotated, legit.Outcome)
	reuse := e.rotate(t, store, stolen.Raw) // the thief replays the old token

	assert.Equal(t, auth.Reused, reuse.Outcome)
	active, err := store.SessionActive(ctx, e.db.Pool(), u.ID, stolen.FamilyID)
	require.NoError(t, err)
	assert.False(t, active, "the whole session is dead, including the newest token")
	assert.Equal(t, auth.Reused, e.rotate(t, store, legit.Token.Raw).Outcome, "the legitimate newest token is dead too")

	active, err = store.SessionActive(ctx, e.db.Pool(), u.ID, otherLogin.FamilyID)
	require.NoError(t, err)
	assert.True(t, active, "other devices keep their sessions")
	assert.Equal(t, auth.Rotated, e.rotate(t, store, otherLogin.Raw).Outcome)
}

func TestRotate_RevocationFromReuseSurvivesAfterCommit(t *testing.T) {
	e := newEnv(t)
	u := e.newUser(t)
	store := auth.NewRefreshTokens(e.clock, refreshTTL)
	tok, err := store.StartSession(context.Background(), e.db.Pool(), u.ID, auth.ClientInfo{})
	require.NoError(t, err)
	e.rotate(t, store, tok.Raw)
	e.rotate(t, store, tok.Raw) // reuse

	var live int
	require.NoError(t, e.db.Pool().QueryRow(context.Background(), "SELECT count(*) FROM refresh_tokens WHERE revoked_at IS NULL").Scan(&live))
	assert.Zero(t, live, "reuse detection persisted: no live token is left in the family")
}

func TestRotate_RolledBackTransactionLeavesTheTokenUsable(t *testing.T) {
	e := newEnv(t)
	u := e.newUser(t)
	store := auth.NewRefreshTokens(e.clock, refreshTTL)
	ctx := context.Background()
	tok, err := store.StartSession(ctx, e.db.Pool(), u.ID, auth.ClientInfo{})
	require.NoError(t, err)

	err = e.db.WithTx(ctx, func(tx pgx.Tx) error {
		res, err := store.Rotate(ctx, tx, tok.Raw, auth.ClientInfo{})
		require.NoError(t, err)
		require.Equal(t, auth.Rotated, res.Outcome)
		return assert.AnError // the caller fails after rotating
	})
	require.Error(t, err)

	assert.Equal(t, auth.Rotated, e.rotate(t, store, tok.Raw).Outcome, "the failed attempt did not burn the token")
}

func TestRotate_ConcurrentUseOfOneTokenHasExactlyOneWinner(t *testing.T) {
	e := newEnv(t)
	u := e.newUser(t)
	store := auth.NewRefreshTokens(e.clock, refreshTTL)
	tok, err := store.StartSession(context.Background(), e.db.Pool(), u.ID, auth.ClientInfo{})
	require.NoError(t, err)

	var rotated, reused atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var res auth.RotateResult
			err := e.db.WithTx(context.Background(), func(tx pgx.Tx) error {
				var err error
				res, err = store.Rotate(context.Background(), tx, tok.Raw, auth.ClientInfo{})
				return err
			})
			if !assert.NoError(t, err) {
				return
			}
			switch res.Outcome {
			case auth.Rotated:
				rotated.Add(1)
			case auth.Reused:
				reused.Add(1)
			}
		}()
	}
	wg.Wait()

	assert.EqualValues(t, 1, rotated.Load(), "a refresh token can be exchanged exactly once")
	assert.EqualValues(t, 7, reused.Load(), "concurrent replays count as reuse (spec §5.3)")
	active, err := store.SessionActive(context.Background(), e.db.Pool(), u.ID, tok.FamilyID)
	require.NoError(t, err)
	assert.False(t, active, "and the session is revoked as a precaution")
}

func TestRevokeOtherSessions_KeepsOnlyTheNamedSessionAndOnlyForThatUser(t *testing.T) {
	e := newEnv(t)
	u, other := e.newUser(t), e.newUser(t)
	store := auth.NewRefreshTokens(e.clock, refreshTTL)
	ctx := context.Background()
	keep, err := store.StartSession(ctx, e.db.Pool(), u.ID, auth.ClientInfo{})
	require.NoError(t, err)
	laptop, err := store.StartSession(ctx, e.db.Pool(), u.ID, auth.ClientInfo{})
	require.NoError(t, err)
	phone, err := store.StartSession(ctx, e.db.Pool(), u.ID, auth.ClientInfo{})
	require.NoError(t, err)
	strangers, err := store.StartSession(ctx, e.db.Pool(), other.ID, auth.ClientInfo{})
	require.NoError(t, err)

	require.NoError(t, store.RevokeOtherSessions(ctx, e.db.Pool(), u.ID, keep.FamilyID))

	for session, want := range map[uuid.UUID]bool{keep.FamilyID: true, laptop.FamilyID: false, phone.FamilyID: false} {
		active, err := store.SessionActive(ctx, e.db.Pool(), u.ID, session)
		require.NoError(t, err)
		assert.Equal(t, want, active)
	}
	active, err := store.SessionActive(ctx, e.db.Pool(), other.ID, strangers.FamilyID)
	require.NoError(t, err)
	assert.True(t, active, "another user's sessions are untouched")
}
