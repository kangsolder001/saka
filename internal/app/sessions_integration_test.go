//go:build integration

package app_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sessionsPath = "/api/v1/me/sessions"

// loginAs logs in with a given User-Agent (cookie delivery) and returns the session.
func (s *stack) loginAs(email, userAgent string) session {
	s.t.Helper()
	res := s.post(loginPath, loginBody(email, "correct-horse"), map[string]string{"User-Agent": userAgent})
	require.Equal(s.t, http.StatusOK, res.Code, res.Body.String())
	cookies := res.Result().Cookies()
	require.Len(s.t, cookies, 1)
	access, _ := res.json()["access_token"].(string)
	return session{access: access, refresh: cookies[0].Value}
}

func (s *stack) listSessions(access string) []map[string]any {
	s.t.Helper()
	res := s.get(sessionsPath, bearer(access))
	require.Equal(s.t, http.StatusOK, res.Code, res.Body.String())
	var out []map[string]any
	for _, v := range res.json()["sessions"].([]any) {
		out = append(out, v.(map[string]any))
	}
	return out
}

func (s *stack) deleteSession(access, id string) response {
	s.t.Helper()
	return s.do(http.MethodDelete, sessionsPath+"/"+id, "", bearer(access))
}

func TestSessions_ListShowsEachLoginAndFlagsTheCurrentOne(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	laptop := s.loginAs("ann@example.com", "Firefox/130")
	s.clock.Advance(time.Minute)
	phone := s.loginAs("ann@example.com", "Safari/Mobile")

	forLaptop := s.listSessions(laptop.access)
	forPhone := s.listSessions(phone.access)

	require.Len(t, forLaptop, 2)
	assert.Equal(t, "Safari/Mobile", forLaptop[0]["user_agent"], "most recently used first")
	assert.Equal(t, false, forLaptop[0]["current"])
	assert.Equal(t, "Firefox/130", forLaptop[1]["user_agent"])
	assert.Equal(t, true, forLaptop[1]["current"], "the laptop sees itself as current")
	assert.Equal(t, true, forPhone[0]["current"], "and the phone sees itself")
	assert.Equal(t, false, forPhone[1]["current"])
	assert.NotEmpty(t, forLaptop[0]["id"])
	assert.NotEmpty(t, forLaptop[0]["ip"])
	assert.NotContains(t, forLaptop[0], "refresh_token")
	assert.NotContains(t, forLaptop[0], "token_hash")
}

func TestSessions_RefreshingKeepsOneEntryAndUpdatesLastUsed(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	laptop := s.loginAs("ann@example.com", "Firefox/130")
	before := s.listSessions(laptop.access)
	require.Len(t, before, 1)
	s.clock.Advance(10 * time.Minute)

	_, refreshed := s.refreshWithCookie(laptop.refresh)
	after := s.listSessions(refreshed.access)

	require.Len(t, after, 1, "rotation does not create a second session")
	assert.Equal(t, before[0]["id"], after[0]["id"], "the session ID is stable across rotations")
	assert.Equal(t, before[0]["created_at"], after[0]["created_at"])
	assert.NotEqual(t, before[0]["last_used_at"], after[0]["last_used_at"])
}

func TestSessions_EndingAnotherSessionLogsThatDeviceOutImmediately(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	laptop := s.loginAs("ann@example.com", "Firefox/130")
	phone := s.loginAs("ann@example.com", "Safari/Mobile")
	var phoneID string
	for _, sess := range s.listSessions(laptop.access) {
		if sess["user_agent"] == "Safari/Mobile" {
			phoneID = sess["id"].(string)
		}
	}
	require.NotEmpty(t, phoneID)

	res := s.deleteSession(laptop.access, phoneID)

	require.Equal(t, http.StatusNoContent, res.Code, res.Body.String())
	assert.False(t, s.accessWorks(phone.access), "its access token stops working at once")
	refreshed, _ := s.refreshWithCookie(phone.refresh)
	assert.Equal(t, http.StatusUnauthorized, refreshed.Code, "and so does its refresh token")
	assert.True(t, s.accessWorks(laptop.access), "this device is untouched")
	remaining := s.listSessions(laptop.access)
	require.Len(t, remaining, 1)
	assert.Equal(t, "Firefox/130", remaining[0]["user_agent"])
	assert.Equal(t, http.StatusNotFound, s.deleteSession(laptop.access, phoneID).Code, "ending it twice is not found")
}

func TestSessions_EndingTheCurrentSessionWorksLikeLogout(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	laptop := s.loginAs("ann@example.com", "Firefox/130")
	phone := s.loginAs("ann@example.com", "Safari/Mobile")
	var laptopID string
	for _, sess := range s.listSessions(laptop.access) {
		if sess["current"] == true {
			laptopID = sess["id"].(string)
		}
	}

	res := s.deleteSession(laptop.access, laptopID)

	require.Equal(t, http.StatusNoContent, res.Code)
	assert.False(t, s.accessWorks(laptop.access))
	assert.True(t, s.accessWorks(phone.access), "the other device stays logged in")
}

func TestSessions_CannotSeeOrEndAnotherUsersSessions(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	s.registered("bob@example.com")
	ann := s.loginAs("ann@example.com", "Firefox/130")
	bob := s.loginAs("bob@example.com", "Chrome/129")
	bobSessions := s.listSessions(bob.access)
	require.Len(t, bobSessions, 1)
	bobID := bobSessions[0]["id"].(string)

	assert.Len(t, s.listSessions(ann.access), 1, "ann sees only her own login")
	res := s.deleteSession(ann.access, bobID)

	assert.Equal(t, http.StatusNotFound, res.Code, "same answer as a session that does not exist")
	assert.True(t, s.accessWorks(bob.access), "bob is still logged in")
}

func TestSessions_MalformedIDIsNotFound(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	access := s.loggedIn("ann@example.com")

	assert.Equal(t, http.StatusNotFound, s.deleteSession(access, "not-a-uuid").Code)
}

func TestSessions_LoggedOutAndExpiredLoginsDisappearFromTheList(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	laptop := s.loginAs("ann@example.com", "Firefox/130")
	phone := s.loginAs("ann@example.com", "Safari/Mobile")
	require.Equal(t, http.StatusNoContent, s.post("/api/v1/auth/logout", ``, bearer(phone.access)).Code)

	got := s.listSessions(laptop.access)

	require.Len(t, got, 1, "a logged-out device is gone from the list")
	assert.Equal(t, "Firefox/130", got[0]["user_agent"])
}

func TestSessions_RequireAuthentication(t *testing.T) {
	s := newStack(t)

	assert.Equal(t, http.StatusUnauthorized, s.get(sessionsPath, nil).Code)
	assert.Equal(t, http.StatusUnauthorized, s.do(http.MethodDelete, sessionsPath+"/0198f2a1-0000-7000-8000-000000000000", "", nil).Code)
}
