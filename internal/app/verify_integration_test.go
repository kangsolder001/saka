//go:build integration

package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const verifyPath = "/api/v1/auth/verify-email"

// latestEmailToken returns the raw token from the most recent queued email to
// the given address: the same link the user would click.
func (s *stack) latestEmailToken(to string) string {
	s.t.Helper()
	var payload []byte
	require.NoError(s.t, s.queryRow(
		`SELECT payload FROM outbox_jobs WHERE type = 'email.send' AND payload->>'to' = $1
		 ORDER BY created_at DESC, id DESC LIMIT 1`, to).Scan(&payload))
	var job struct {
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	require.NoError(s.t, json.Unmarshal(payload, &job))
	link, err := url.Parse(job.Data.URL)
	require.NoError(s.t, err)
	token := link.Query().Get("token")
	require.NotEmpty(s.t, token, "email link has no token: %s", job.Data.URL)
	return token
}

func (s *stack) emailVerified(email string) bool {
	s.t.Helper()
	var verified bool
	require.NoError(s.t, s.queryRow("SELECT email_verified_at IS NOT NULL FROM users WHERE email = $1", email).Scan(&verified))
	return verified
}

func TestVerifyEmail_EndToEnd(t *testing.T) {
	s := newStack(t)
	require.Equal(t, http.StatusCreated, s.post(registerPath, registerBody("ann@example.com")).Code)
	require.False(t, s.emailVerified("ann@example.com"))
	token := s.latestEmailToken("ann@example.com")

	res := s.post(verifyPath, `{"token":"`+token+`"}`)

	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	assert.True(t, s.emailVerified("ann@example.com"))
}

func TestVerifyEmail_TokenWorksOnlyOnce(t *testing.T) {
	s := newStack(t)
	require.Equal(t, http.StatusCreated, s.post(registerPath, registerBody("ann@example.com")).Code)
	token := s.latestEmailToken("ann@example.com")
	require.Equal(t, http.StatusOK, s.post(verifyPath, `{"token":"`+token+`"}`).Code)

	again := s.post(verifyPath, `{"token":"`+token+`"}`)

	assert.Equal(t, http.StatusBadRequest, again.Code)
}

func TestVerifyEmail_ExpiredTokenIsRejected(t *testing.T) {
	s := newStack(t)
	require.Equal(t, http.StatusCreated, s.post(registerPath, registerBody("ann@example.com")).Code)
	token := s.latestEmailToken("ann@example.com")

	s.clock.Advance(24*time.Hour + time.Second)
	res := s.post(verifyPath, `{"token":"`+token+`"}`)

	assert.Equal(t, http.StatusBadRequest, res.Code)
	assert.False(t, s.emailVerified("ann@example.com"))
}

func TestVerifyEmail_UnknownAndWrongTypeTokensAreRejected(t *testing.T) {
	s := newStack(t)
	require.Equal(t, http.StatusCreated, s.post(registerPath, registerBody("ann@example.com")).Code)
	var userID string
	require.NoError(t, s.queryRow("SELECT id FROM users").Scan(&userID))
	// A password-reset token must never verify an email.
	_, err := s.db.Pool().Exec(context.Background(),
		`INSERT INTO user_tokens (user_id, type, token_hash, expires_at)
		 VALUES ($1, 'password_reset', $2, now() + interval '1 hour')`, userID, authHash("reset-token"))
	require.NoError(t, err)

	assert.Equal(t, http.StatusBadRequest, s.post(verifyPath, `{"token":"made-up"}`).Code)
	assert.Equal(t, http.StatusBadRequest, s.post(verifyPath, `{"token":"reset-token"}`).Code)
	assert.False(t, s.emailVerified("ann@example.com"))
}
