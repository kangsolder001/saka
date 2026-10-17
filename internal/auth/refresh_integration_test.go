//go:build integration

package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
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
