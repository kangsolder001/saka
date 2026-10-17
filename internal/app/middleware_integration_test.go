//go:build integration

package app_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const resendPath = "/api/v1/auth/resend-verification"

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

// loggedIn registers (if needed) and logs in, returning the access token.
func (s *stack) loggedIn(email string) string {
	s.t.Helper()
	res := s.post(loginPath, loginBody(email, "correct-horse"))
	require.Equal(s.t, http.StatusOK, res.Code, res.Body.String())
	token, _ := res.json()["access_token"].(string)
	require.NotEmpty(s.t, token)
	return token
}

func TestResendVerification_EndToEnd(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	access := s.loggedIn("ann@example.com")
	oldToken := s.latestEmailToken("ann@example.com")

	res := s.post(resendPath, ``, bearer(access))

	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	newToken := s.latestEmailToken("ann@example.com")
	assert.NotEqual(t, oldToken, newToken, "a fresh link was emailed")
	assert.Equal(t, http.StatusBadRequest, s.post(verifyPath, `{"token":"`+oldToken+`"}`).Code, "the older link no longer works")
	assert.Equal(t, http.StatusOK, s.post(verifyPath, `{"token":"`+newToken+`"}`).Code)

	again := s.post(resendPath, ``, bearer(access))
	assert.Equal(t, http.StatusConflict, again.Code, "already verified")
}

func TestResendVerification_RequiresAValidAccessToken(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")

	assert.Equal(t, http.StatusUnauthorized, s.post(resendPath, ``).Code)
	assert.Equal(t, http.StatusUnauthorized, s.post(resendPath, ``, bearer("garbage")).Code)
	var jobs int
	require.NoError(t, s.queryRow("SELECT count(*) FROM outbox_jobs").Scan(&jobs))
	assert.Equal(t, 1, jobs, "only the registration email exists")
}

func TestAccessToken_ExpiresAfterFifteenMinutes(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	access := s.loggedIn("ann@example.com")

	s.clock.Advance(15*time.Minute - time.Second)
	assert.Equal(t, http.StatusOK, s.post(resendPath, ``, bearer(access)).Code)

	s.clock.Advance(2 * time.Second)
	res := s.post(resendPath, ``, bearer(access))
	assert.Equal(t, http.StatusUnauthorized, res.Code)
	assert.Contains(t, res.Body.String(), `"code":"TOKEN_EXPIRED"`)
}

func TestAccessToken_StopsWorkingImmediatelyWhenTheSessionIsRevoked(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	access := s.loggedIn("ann@example.com")
	require.Equal(t, http.StatusOK, s.post(resendPath, ``, bearer(access)).Code)

	_, err := s.db.Pool().Exec(context.Background(), "UPDATE refresh_tokens SET revoked_at = now()")
	require.NoError(t, err)

	res := s.post(resendPath, ``, bearer(access))
	assert.Equal(t, http.StatusUnauthorized, res.Code, "no waiting for the 15 minute token to run out")
	assert.Contains(t, res.Body.String(), `"code":"UNAUTHENTICATED"`)
}

func TestAccessToken_StopsWorkingImmediatelyWhenTheUserIsSuspendedOrDeleted(t *testing.T) {
	for name, statement := range map[string]string{
		"suspended": "UPDATE users SET status = 'suspended'",
		"deleted":   "UPDATE users SET status = 'deleted', deleted_at = now()",
	} {
		t.Run(name, func(t *testing.T) {
			s := newStack(t)
			s.registered("ann@example.com")
			access := s.loggedIn("ann@example.com")
			_, err := s.db.Pool().Exec(context.Background(), statement)
			require.NoError(t, err)

			res := s.post(resendPath, ``, bearer(access))

			assert.Equal(t, http.StatusUnauthorized, res.Code)
		})
	}
}

func TestAccessToken_OfAnotherUsersSessionCannotBeReused(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	s.registered("bob@example.com")
	annAccess := s.loggedIn("ann@example.com")
	// Revoke only Bob's sessions: Ann must be unaffected.
	_, err := s.db.Pool().Exec(context.Background(),
		"UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = (SELECT id FROM users WHERE email = 'bob@example.com')")
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, s.post(resendPath, ``, bearer(annAccess)).Code)
}
