//go:build integration

package app_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/auth"
)

const loginPath = "/api/v1/auth/login"

func loginBody(email, password string) string {
	return `{"email":"` + email + `","password":"` + password + `"}`
}

// registered creates an account through the API and returns its email.
func (s *stack) registered(email string) string {
	s.t.Helper()
	require.Equal(s.t, http.StatusCreated, s.post(registerPath, registerBody(email)).Code)
	return email
}

func TestLogin_EndToEnd_UnverifiedUserGetsSessionWithCookie(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")

	res := s.post(loginPath, loginBody("ann@example.com", "correct-horse"), map[string]string{"User-Agent": "TestAgent/1.0"})

	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	body := res.json()
	assert.Equal(t, "Bearer", body["token_type"])
	assert.EqualValues(t, 900, body["expires_in"])
	assert.Equal(t, false, body["user"].(map[string]any)["email_verified"], "unverified users may log in (spec §5.2)")
	assert.NotContains(t, body, "refresh_token")

	cookies := res.Result().Cookies()
	require.Len(t, cookies, 1)
	cookie := cookies[0]
	assert.Equal(t, auth.RefreshCookieName, cookie.Name)
	assert.True(t, cookie.HttpOnly)

	// Only the hash of the refresh token is stored, with the client's details.
	var hash, userAgent, ip string
	require.NoError(t, s.queryRow("SELECT token_hash, user_agent, ip FROM refresh_tokens").Scan(&hash, &userAgent, &ip))
	assert.Equal(t, auth.HashToken(cookie.Value), hash)
	assert.NotEqual(t, cookie.Value, hash)
	assert.Equal(t, "TestAgent/1.0", userAgent)
	assert.NotEmpty(t, ip)

	// The access token is bound to the refresh token's family.
	claims, err := auth.NewJWT("0123456789abcdef0123456789abcdef-e2e", 15*time.Minute, s.clock)
	require.NoError(t, err)
	verified, err := claims.Verify(body["access_token"].(string))
	require.NoError(t, err)
	var family, userID string
	require.NoError(t, s.queryRow("SELECT family_id::text, user_id::text FROM refresh_tokens").Scan(&family, &userID))
	assert.Equal(t, family, verified.SessionID.String())
	assert.Equal(t, userID, verified.UserID.String())
}

func TestLogin_BodyDelivery(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")

	res := s.post(loginPath, `{"email":"ann@example.com","password":"correct-horse","token_delivery":"body"}`)

	require.Equal(t, http.StatusOK, res.Code)
	refresh, _ := res.json()["refresh_token"].(string)
	assert.NotEmpty(t, refresh)
	assert.Empty(t, res.Result().Cookies())
	var n int
	require.NoError(t, s.queryRow("SELECT count(*) FROM refresh_tokens WHERE token_hash = $1", auth.HashToken(refresh)).Scan(&n))
	assert.Equal(t, 1, n)
}

func TestLogin_NoUserEnumeration(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")

	wrongPassword := s.post(loginPath, loginBody("ann@example.com", "wrong-password"))
	unknownEmail := s.post(loginPath, loginBody("ghost@example.com", "wrong-password"))

	assert.Equal(t, http.StatusUnauthorized, wrongPassword.Code)
	assert.Equal(t, wrongPassword.Code, unknownEmail.Code)
	assert.JSONEq(t, wrongPassword.Body.String(), unknownEmail.Body.String(), "bodies must be identical")
	var sessions int
	require.NoError(t, s.queryRow("SELECT count(*) FROM refresh_tokens").Scan(&sessions))
	assert.Zero(t, sessions, "failed logins create no session")
}

func TestLogin_EmailIsCaseInsensitive(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")

	res := s.post(loginPath, loginBody("ANN@Example.COM", "correct-horse"))

	assert.Equal(t, http.StatusOK, res.Code)
}

func TestLogin_SuspendedAccount(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	_, err := s.admin.Exec(context.Background(), "UPDATE users SET status = 'suspended'")
	require.NoError(t, err)

	good := s.post(loginPath, loginBody("ann@example.com", "correct-horse"))
	bad := s.post(loginPath, loginBody("ann@example.com", "wrong-password"))

	assert.Equal(t, http.StatusForbidden, good.Code)
	assert.Equal(t, http.StatusUnauthorized, bad.Code, "a wrong password never reveals the account state")
}

func TestLogin_EachLoginStartsItsOwnSession(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")

	require.Equal(t, http.StatusOK, s.post(loginPath, loginBody("ann@example.com", "correct-horse")).Code)
	require.Equal(t, http.StatusOK, s.post(loginPath, loginBody("ann@example.com", "correct-horse")).Code)

	var families int
	require.NoError(t, s.queryRow("SELECT count(DISTINCT family_id) FROM refresh_tokens").Scan(&families))
	assert.Equal(t, 2, families)
}
