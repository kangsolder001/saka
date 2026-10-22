//go:build integration

package app_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/auth"
)

const refreshPath = "/api/v1/auth/refresh"

func cookieHeader(value string) map[string]string {
	return map[string]string{"Cookie": auth.RefreshCookieName + "=" + value}
}

// session is one logged-in client using cookie delivery.
type session struct {
	access  string
	refresh string
}

func (s *stack) cookieLogin(email string) session {
	s.t.Helper()
	res := s.post(loginPath, loginBody(email, "correct-horse"))
	require.Equal(s.t, http.StatusOK, res.Code, res.Body.String())
	cookies := res.Result().Cookies()
	require.Len(s.t, cookies, 1)
	access, _ := res.json()["access_token"].(string)
	return session{access: access, refresh: cookies[0].Value}
}

// refreshWithCookie calls /auth/refresh and returns the response plus the new session.
func (s *stack) refreshWithCookie(refresh string) (response, session) {
	s.t.Helper()
	res := s.post(refreshPath, ``, cookieHeader(refresh))
	if res.Code != http.StatusOK {
		return res, session{}
	}
	access, _ := res.json()["access_token"].(string)
	cookies := res.Result().Cookies()
	require.Len(s.t, cookies, 1)
	return res, session{access: access, refresh: cookies[0].Value}
}

func (s *stack) accessWorks(access string) bool {
	s.t.Helper()
	// resend-verification is a cheap authenticated route; 200 or 409 both mean
	// "authenticated", 401 means rejected.
	return s.post(resendPath, ``, bearer(access)).Code != http.StatusUnauthorized
}

func TestRefresh_RotatesCookieAndIssuesWorkingAccessToken(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	first := s.cookieLogin("ann@example.com")

	res, second := s.refreshWithCookie(first.refresh)

	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	assert.NotEqual(t, first.refresh, second.refresh, "the refresh token is rotated")
	assert.NotContains(t, res.Body.String(), second.refresh)
	assert.True(t, s.accessWorks(second.access))
	var live int
	require.NoError(t, s.queryRow("SELECT count(*) FROM refresh_tokens WHERE revoked_at IS NULL").Scan(&live))
	assert.Equal(t, 1, live)
	var families int
	require.NoError(t, s.queryRow("SELECT count(DISTINCT family_id) FROM refresh_tokens").Scan(&families))
	assert.Equal(t, 1, families, "refreshing continues the same session")
}

func TestRefresh_BodyClientsGetTheNewTokenInTheBody(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	login := s.post(loginPath, `{"email":"ann@example.com","password":"correct-horse","token_delivery":"body"}`)
	require.Equal(t, http.StatusOK, login.Code)
	oldRefresh, _ := login.json()["refresh_token"].(string)

	res := s.post(refreshPath, `{"refresh_token":"`+oldRefresh+`"}`)

	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	newRefresh, _ := res.json()["refresh_token"].(string)
	assert.NotEmpty(t, newRefresh)
	assert.NotEqual(t, oldRefresh, newRefresh)
	assert.Empty(t, res.Result().Cookies())
}

func TestRefresh_ReusingARotatedTokenKillsTheWholeSession(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	original := s.cookieLogin("ann@example.com")
	_, rotated := s.refreshWithCookie(original.refresh) // the legitimate refresh
	require.True(t, s.accessWorks(rotated.access))

	// An attacker replays the original (now retired) token.
	replay, _ := s.refreshWithCookie(original.refresh)

	assert.Equal(t, http.StatusUnauthorized, replay.Code)
	cleared := replay.Result().Cookies()
	require.Len(t, cleared, 1)
	assert.Negative(t, cleared[0].MaxAge, "the cookie is deleted in the browser")

	assert.False(t, s.accessWorks(rotated.access), "access tokens of that session stop working at once")
	again, _ := s.refreshWithCookie(rotated.refresh)
	assert.Equal(t, http.StatusUnauthorized, again.Code, "the legitimate user must log in again")
}

func TestRefresh_ReuseOnOneDeviceDoesNotLogOutAnotherDevice(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	phone := s.cookieLogin("ann@example.com")
	laptop := s.cookieLogin("ann@example.com")
	_, _ = s.refreshWithCookie(phone.refresh)
	s.refreshWithCookie(phone.refresh) // replay kills the phone session

	assert.False(t, s.accessWorks(phone.access))
	assert.True(t, s.accessWorks(laptop.access))
	res, _ := s.refreshWithCookie(laptop.refresh)
	assert.Equal(t, http.StatusOK, res.Code)
}

func TestRefresh_UnknownExpiredAndMissingTokens(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	sess := s.cookieLogin("ann@example.com")

	unknown, _ := s.refreshWithCookie("made-up-token")
	assert.Equal(t, http.StatusUnauthorized, unknown.Code)

	missing := s.post(refreshPath, ``)
	assert.Equal(t, http.StatusBadRequest, missing.Code)

	s.clock.Advance(30*24*time.Hour + time.Second)
	expired, _ := s.refreshWithCookie(sess.refresh)
	assert.Equal(t, http.StatusUnauthorized, expired.Code)
}

func TestRefresh_AlwaysFailsTheSameWayWhateverTheReason(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	used := s.cookieLogin("ann@example.com")
	s.refreshWithCookie(used.refresh) // `used` is now retired
	fresh := s.cookieLogin("ann@example.com")
	s.clock.Advance(31 * 24 * time.Hour)

	unknown, _ := s.refreshWithCookie("nope")
	expired, _ := s.refreshWithCookie(fresh.refresh)
	reused, _ := s.refreshWithCookie(used.refresh)

	assert.JSONEq(t, unknown.Body.String(), expired.Body.String())
	assert.JSONEq(t, unknown.Body.String(), reused.Body.String(), "the response must not tell an attacker which case they hit")
}

func TestRefresh_SuspendedUserCannotRefresh(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	sess := s.cookieLogin("ann@example.com")
	_, err := s.admin.Exec(t.Context(), "UPDATE users SET status = 'suspended'")
	require.NoError(t, err)

	res, _ := s.refreshWithCookie(sess.refresh)

	assert.Equal(t, http.StatusUnauthorized, res.Code)
}
