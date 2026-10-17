//go:build integration

package app_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const changePasswordPath = "/api/v1/me/password"

func changePasswordBody(current, next string) string {
	return `{"current_password":"` + current + `","new_password":"` + next + `"}`
}

func TestChangePassword_EndToEnd_KeepsThisSessionAndEndsTheOthers(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	laptop := s.cookieLogin("ann@example.com")
	phone := s.cookieLogin("ann@example.com")
	require.True(t, s.accessWorks(laptop.access))
	require.True(t, s.accessWorks(phone.access))

	res := s.post(changePasswordPath, changePasswordBody("correct-horse", "brand-new-password"), bearer(phone.access))

	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	assert.True(t, s.accessWorks(phone.access), "the session that changed the password stays logged in")
	assert.False(t, s.accessWorks(laptop.access), "every other session ends immediately")
	refreshed, _ := s.refreshWithCookie(laptop.refresh)
	assert.Equal(t, http.StatusUnauthorized, refreshed.Code, "and cannot be refreshed")
	stillGood, _ := s.refreshWithCookie(phone.refresh)
	assert.Equal(t, http.StatusOK, stillGood.Code, "the current session's refresh token still works")
	assert.Equal(t, http.StatusOK, s.loginStatus("ann@example.com", "brand-new-password"))
	assert.Equal(t, http.StatusUnauthorized, s.loginStatus("ann@example.com", "correct-horse"))
	assert.Equal(t, 1, s.emailsTo("ann@example.com", "password_changed"), "the user is told")
}

func TestChangePassword_WrongCurrentPasswordChangesNothing(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	laptop := s.cookieLogin("ann@example.com")
	phone := s.cookieLogin("ann@example.com")

	res := s.post(changePasswordPath, changePasswordBody("wrong-password", "brand-new-password"), bearer(phone.access))

	require.Equal(t, http.StatusBadRequest, res.Code, res.Body.String())
	assert.Contains(t, res.Body.String(), `"current_password":"is incorrect"`)
	assert.True(t, s.accessWorks(laptop.access), "no session was touched")
	assert.Equal(t, http.StatusOK, s.loginStatus("ann@example.com", "correct-horse"), "the password is unchanged")
	assert.Zero(t, s.emailsTo("ann@example.com", "password_changed"))
}

func TestChangePassword_GuessingTheCurrentPasswordIsRateLimited(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	access := s.loggedIn("ann@example.com")

	for range 5 {
		res := s.post(changePasswordPath, changePasswordBody("wrong-password", "brand-new-password"), bearer(access))
		require.Equal(t, http.StatusBadRequest, res.Code)
	}

	locked := s.post(changePasswordPath, changePasswordBody("wrong-password", "brand-new-password"), bearer(access))
	assert.Equal(t, http.StatusTooManyRequests, locked.Code)
	assert.NotEmpty(t, locked.Header().Get("Retry-After"))
	rightButLocked := s.post(changePasswordPath, changePasswordBody("correct-horse", "brand-new-password"), bearer(access))
	assert.Equal(t, http.StatusTooManyRequests, rightButLocked.Code, "the right password is refused too while locked")
}

func TestChangePassword_SuccessClearsEarlierFailures(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	access := s.loggedIn("ann@example.com")
	for range 4 {
		require.Equal(t, http.StatusBadRequest, s.post(changePasswordPath, changePasswordBody("wrong", "brand-new-password"), bearer(access)).Code)
	}
	require.Equal(t, http.StatusOK, s.post(changePasswordPath, changePasswordBody("correct-horse", "brand-new-password"), bearer(access)).Code)

	// Four more wrong guesses would have hit the limit had the first four still counted.
	for range 4 {
		res := s.post(changePasswordPath, changePasswordBody("wrong", "another-password-1"), bearer(access))
		require.Equal(t, http.StatusBadRequest, res.Code, res.Body.String())
	}
}

func TestChangePassword_RejectsWeakOrUnchangedPasswords(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	access := s.loggedIn("ann@example.com")

	for name, body := range map[string]string{
		"too short": changePasswordBody("correct-horse", "short"),
		"unchanged": changePasswordBody("correct-horse", "correct-horse"),
		"the email": changePasswordBody("correct-horse", "ann@example.com"),
	} {
		res := s.post(changePasswordPath, body, bearer(access))

		assert.Equal(t, http.StatusBadRequest, res.Code, "%s: %s", name, res.Body.String())
		assert.Contains(t, res.Body.String(), "new_password", name)
	}
	assert.Zero(t, s.emailsTo("ann@example.com", "password_changed"))
}

func TestChangePassword_RequiresAuthentication(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")

	res := s.post(changePasswordPath, changePasswordBody("correct-horse", "brand-new-password"))

	assert.Equal(t, http.StatusUnauthorized, res.Code)
	assert.Equal(t, http.StatusOK, s.loginStatus("ann@example.com", "correct-horse"))
}
