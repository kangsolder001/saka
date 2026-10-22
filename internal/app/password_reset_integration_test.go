//go:build integration

package app_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	forgotPath = "/api/v1/auth/forgot-password"
	resetPath  = "/api/v1/auth/reset-password"
)

func (s *stack) emailsTo(to, template string) int {
	s.t.Helper()
	var n int
	require.NoError(s.t, s.queryRow(
		"SELECT count(*) FROM outbox_jobs WHERE payload->>'to' = $1 AND payload->>'template' = $2", to, template).Scan(&n))
	return n
}

func (s *stack) loginStatus(email, password string) int {
	s.t.Helper()
	return s.post(loginPath, loginBody(email, password)).Code
}

func TestPasswordReset_EndToEnd(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	oldSession := s.cookieLogin("ann@example.com")
	require.True(t, s.accessWorks(oldSession.access))

	forgot := s.post(forgotPath, `{"email":"ann@example.com"}`)
	require.Equal(t, http.StatusOK, forgot.Code, forgot.Body.String())
	require.Equal(t, 1, s.emailsTo("ann@example.com", "password_reset"))
	token := s.latestEmailToken("ann@example.com")

	reset := s.post(resetPath, `{"token":"`+token+`","password":"brand-new-password"}`)
	require.Equal(t, http.StatusOK, reset.Code, reset.Body.String())

	assert.Equal(t, http.StatusOK, s.loginStatus("ann@example.com", "brand-new-password"), "the new password works")
	assert.Equal(t, http.StatusUnauthorized, s.loginStatus("ann@example.com", "correct-horse"), "the old password no longer works")
	assert.False(t, s.accessWorks(oldSession.access), "existing sessions are revoked immediately")
	refreshed, _ := s.refreshWithCookie(oldSession.refresh)
	assert.Equal(t, http.StatusUnauthorized, refreshed.Code, "existing refresh tokens are revoked")
	assert.Equal(t, 1, s.emailsTo("ann@example.com", "password_changed"), "the user is told their password changed")
}

func TestPasswordReset_ResetOnlyEndsThatUsersSessions(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	s.registered("bob@example.com")
	bob := s.cookieLogin("bob@example.com")
	s.post(forgotPath, `{"email":"ann@example.com"}`)
	token := s.latestEmailToken("ann@example.com")

	require.Equal(t, http.StatusOK, s.post(resetPath, `{"token":"`+token+`","password":"brand-new-password"}`).Code)

	assert.True(t, s.accessWorks(bob.access), "other users keep their sessions")
}

func TestPasswordReset_TokenIsSingleUse(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	s.post(forgotPath, `{"email":"ann@example.com"}`)
	token := s.latestEmailToken("ann@example.com")
	require.Equal(t, http.StatusOK, s.post(resetPath, `{"token":"`+token+`","password":"first-new-password"}`).Code)

	again := s.post(resetPath, `{"token":"`+token+`","password":"second-new-password"}`)

	assert.Equal(t, http.StatusBadRequest, again.Code)
	assert.Equal(t, http.StatusOK, s.loginStatus("ann@example.com", "first-new-password"))
	assert.Equal(t, http.StatusUnauthorized, s.loginStatus("ann@example.com", "second-new-password"))
}

func TestPasswordReset_TokenExpiresAfterOneHour(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	s.post(forgotPath, `{"email":"ann@example.com"}`)
	token := s.latestEmailToken("ann@example.com")

	s.clock.Advance(time.Hour + time.Second)
	res := s.post(resetPath, `{"token":"`+token+`","password":"brand-new-password"}`)

	assert.Equal(t, http.StatusBadRequest, res.Code)
	assert.Equal(t, http.StatusOK, s.loginStatus("ann@example.com", "correct-horse"), "the password did not change")
}

func TestPasswordReset_WeakPasswordDoesNotBurnTheToken(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	s.post(forgotPath, `{"email":"ann@example.com"}`)
	token := s.latestEmailToken("ann@example.com")

	weak := s.post(resetPath, `{"token":"`+token+`","password":"short"}`)
	sameAsEmail := s.post(resetPath, `{"token":"`+token+`","password":"ann@example.com"}`)
	good := s.post(resetPath, `{"token":"`+token+`","password":"brand-new-password"}`)

	assert.Equal(t, http.StatusBadRequest, weak.Code)
	assert.Equal(t, http.StatusBadRequest, sameAsEmail.Code, "a password equal to the email is rejected")
	assert.Equal(t, http.StatusOK, good.Code, "the user can retry with the same link after a rejected password")
}

func TestPasswordReset_NewRequestInvalidatesTheOlderLink(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	s.post(forgotPath, `{"email":"ann@example.com"}`)
	older := s.latestEmailToken("ann@example.com")
	s.post(forgotPath, `{"email":"ann@example.com"}`)
	newer := s.latestEmailToken("ann@example.com")
	require.NotEqual(t, older, newer)

	assert.Equal(t, http.StatusBadRequest, s.post(resetPath, `{"token":"`+older+`","password":"brand-new-password"}`).Code)
	assert.Equal(t, http.StatusOK, s.post(resetPath, `{"token":"`+newer+`","password":"brand-new-password"}`).Code)
}

func TestPasswordReset_VerificationTokenCannotResetAPassword(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	verificationToken := s.latestEmailToken("ann@example.com") // from the registration email

	res := s.post(resetPath, `{"token":"`+verificationToken+`","password":"brand-new-password"}`)

	assert.Equal(t, http.StatusBadRequest, res.Code, "tokens are bound to their purpose")
}

func TestForgotPassword_DoesNotRevealWhichEmailsAreRegistered(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")

	known := s.post(forgotPath, `{"email":"ann@example.com"}`)
	unknown := s.post(forgotPath, `{"email":"ghost@example.com"}`)

	assert.Equal(t, http.StatusOK, known.Code)
	assert.Equal(t, known.Code, unknown.Code)
	assert.Equal(t, known.Body.String(), unknown.Body.String(), "responses must be identical")
	assert.Equal(t, 1, s.emailsTo("ann@example.com", "password_reset"))
	assert.Zero(t, s.emailsTo("ghost@example.com", "password_reset"), "and nothing is sent to an unregistered address")
}

func TestForgotPassword_SuspendedAccountGetsNoEmailButSameAnswer(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	_, err := s.admin.Exec(t.Context(), "UPDATE users SET status = 'suspended'")
	require.NoError(t, err)

	res := s.post(forgotPath, `{"email":"ann@example.com"}`)

	assert.Equal(t, http.StatusOK, res.Code)
	assert.Zero(t, s.emailsTo("ann@example.com", "password_reset"))
}

func TestForgotPassword_EmailIsCaseInsensitive(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")

	s.post(forgotPath, `{"email":"ANN@Example.com"}`)

	assert.Equal(t, 1, s.emailsTo("ann@example.com", "password_reset"))
}
