//go:build integration

package app_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const logoutPath = "/api/v1/auth/logout"

func TestLogout_EndToEnd(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	sess := s.cookieLogin("ann@example.com")
	require.True(t, s.accessWorks(sess.access))

	res := s.post(logoutPath, ``, bearer(sess.access))

	require.Equal(t, http.StatusNoContent, res.Code, res.Body.String())
	cookies := res.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Negative(t, cookies[0].MaxAge)
	assert.False(t, s.accessWorks(sess.access), "the access token dies at once, not after 15 minutes")
	refresh, _ := s.refreshWithCookie(sess.refresh)
	assert.Equal(t, http.StatusUnauthorized, refresh.Code, "the refresh token is revoked")
	var live int
	require.NoError(t, s.queryRow("SELECT count(*) FROM refresh_tokens WHERE revoked_at IS NULL").Scan(&live))
	assert.Zero(t, live)
}

func TestLogout_OnlyEndsTheCurrentDevice(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	phone := s.cookieLogin("ann@example.com")
	laptop := s.cookieLogin("ann@example.com")

	require.Equal(t, http.StatusNoContent, s.post(logoutPath, ``, bearer(phone.access)).Code)

	assert.False(t, s.accessWorks(phone.access))
	assert.True(t, s.accessWorks(laptop.access))
	res, _ := s.refreshWithCookie(laptop.refresh)
	assert.Equal(t, http.StatusOK, res.Code)
}

func TestLogout_RequiresAValidAccessToken(t *testing.T) {
	s := newStack(t)

	assert.Equal(t, http.StatusUnauthorized, s.post(logoutPath, ``).Code)
	assert.Equal(t, http.StatusUnauthorized, s.post(logoutPath, ``, bearer("garbage")).Code)
}

func TestLogout_CanLogInAgainAfterwards(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	old := s.cookieLogin("ann@example.com")
	require.Equal(t, http.StatusNoContent, s.post(logoutPath, ``, bearer(old.access)).Code)

	fresh := s.cookieLogin("ann@example.com")

	assert.True(t, s.accessWorks(fresh.access))
}
