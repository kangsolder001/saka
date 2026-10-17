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
	changeEmailPath  = "/api/v1/me/email"
	confirmEmailPath = "/api/v1/me/email/confirm"
)

func changeEmailBody(newEmail, password string) string {
	return `{"new_email":"` + newEmail + `","current_password":"` + password + `"}`
}

func (s *stack) currentEmail(access string) string {
	s.t.Helper()
	return s.get(mePath, bearer(access)).json()["user"].(map[string]any)["email"].(string)
}

func TestChangeEmail_EndToEnd_NothingChangesUntilTheNewInboxConfirms(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	access := s.loggedIn("ann@example.com")
	require.Equal(t, "ann@example.com", s.currentEmail(access), "fills the cache")

	res := s.post(changeEmailPath, changeEmailBody("New@Example.com", "correct-horse"), bearer(access))

	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	assert.Equal(t, 1, s.emailsTo("new@example.com", "email_change_confirm"), "the link goes to the NEW address")
	assert.Zero(t, s.emailsTo("ann@example.com", "email_change_confirm"))
	assert.Equal(t, "ann@example.com", s.currentEmail(access), "still the old email until confirmed")
	assert.Equal(t, http.StatusOK, s.loginStatus("ann@example.com", "correct-horse"))

	confirmed := s.post(confirmEmailPath, `{"token":"`+s.latestEmailToken("new@example.com")+`"}`)

	require.Equal(t, http.StatusOK, confirmed.Code, confirmed.Body.String())
	assert.Equal(t, "new@example.com", confirmed.json()["email"])
	assert.Equal(t, "new@example.com", s.currentEmail(access), "the cached profile was dropped at commit")
	assert.True(t, s.emailVerified("new@example.com"), "confirming through the new inbox verifies it")
	assert.Equal(t, 1, s.emailsTo("ann@example.com", "email_changed_notice"), "the OLD address is told")
	assert.Equal(t, http.StatusOK, s.loginStatus("new@example.com", "correct-horse"), "login now uses the new email")
	assert.Equal(t, http.StatusUnauthorized, s.loginStatus("ann@example.com", "correct-horse"))
	assert.True(t, s.accessWorks(access), "the session stays valid")
}

func TestChangeEmail_ConfirmationLinkWorksOnlyOnceAndNeedsNoLogin(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	access := s.loggedIn("ann@example.com")
	require.Equal(t, http.StatusOK, s.post(changeEmailPath, changeEmailBody("new@example.com", "correct-horse"), bearer(access)).Code)
	token := s.latestEmailToken("new@example.com")

	first := s.post(confirmEmailPath, `{"token":"`+token+`"}`) // no Authorization header at all
	again := s.post(confirmEmailPath, `{"token":"`+token+`"}`)

	assert.Equal(t, http.StatusOK, first.Code)
	assert.Equal(t, http.StatusBadRequest, again.Code)
	assert.Equal(t, 1, s.emailsTo("ann@example.com", "email_changed_notice"), "one change, one notice")
}

func TestChangeEmail_AnotherTokenTypeCannotConfirm(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	verification := s.latestEmailToken("ann@example.com") // an email_verification token

	res := s.post(confirmEmailPath, `{"token":"`+verification+`"}`)

	assert.Equal(t, http.StatusBadRequest, res.Code)
	assert.Equal(t, "ann@example.com", s.emailOfFirstUser(), "the email is unchanged")
}

func TestChangeEmail_NewerRequestInvalidatesTheOlderLink(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	access := s.loggedIn("ann@example.com")
	require.Equal(t, http.StatusOK, s.post(changeEmailPath, changeEmailBody("first@example.com", "correct-horse"), bearer(access)).Code)
	oldToken := s.latestEmailToken("first@example.com")
	require.Equal(t, http.StatusOK, s.post(changeEmailPath, changeEmailBody("second@example.com", "correct-horse"), bearer(access)).Code)

	assert.Equal(t, http.StatusBadRequest, s.post(confirmEmailPath, `{"token":"`+oldToken+`"}`).Code, "only the newest link works")
	assert.Equal(t, http.StatusOK, s.post(confirmEmailPath, `{"token":"`+s.latestEmailToken("second@example.com")+`"}`).Code)
	assert.Equal(t, "second@example.com", s.currentEmail(access))
}

func TestChangeEmail_LinkExpiresAfter24Hours(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	access := s.loggedIn("ann@example.com")
	require.Equal(t, http.StatusOK, s.post(changeEmailPath, changeEmailBody("new@example.com", "correct-horse"), bearer(access)).Code)
	token := s.latestEmailToken("new@example.com")

	s.clock.Advance(24*time.Hour + time.Second)

	assert.Equal(t, http.StatusBadRequest, s.post(confirmEmailPath, `{"token":"`+token+`"}`).Code)
	assert.Equal(t, "ann@example.com", s.emailOfFirstUser())
}

func (s *stack) emailOfFirstUser() string {
	s.t.Helper()
	var email string
	require.NoError(s.t, s.queryRow("SELECT email FROM users ORDER BY created_at LIMIT 1").Scan(&email))
	return email
}

func TestChangeEmail_RejectsWrongPasswordSameAndTakenAddresses(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	s.registered("bob@example.com")
	access := s.loggedIn("ann@example.com")

	wrong := s.post(changeEmailPath, changeEmailBody("new@example.com", "wrong-password"), bearer(access))
	same := s.post(changeEmailPath, changeEmailBody("ANN@example.com", "correct-horse"), bearer(access))
	taken := s.post(changeEmailPath, changeEmailBody("bob@example.com", "correct-horse"), bearer(access))

	assert.Equal(t, http.StatusBadRequest, wrong.Code)
	assert.Contains(t, wrong.Body.String(), "current_password")
	assert.Equal(t, http.StatusBadRequest, same.Code)
	assert.Equal(t, http.StatusConflict, taken.Code)
	assert.Zero(t, s.emailsTo("new@example.com", "email_change_confirm"), "nothing was sent")
	assert.Zero(t, s.emailsTo("bob@example.com", "email_change_confirm"))
}

func TestChangeEmail_AddressTakenBeforeConfirmingIsAConflictAndKeepsTheOldEmail(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	access := s.loggedIn("ann@example.com")
	require.Equal(t, http.StatusOK, s.post(changeEmailPath, changeEmailBody("new@example.com", "correct-horse"), bearer(access)).Code)
	token := s.latestEmailToken("new@example.com")
	s.registered("new@example.com") // someone else signs up with it in the meantime

	res := s.post(confirmEmailPath, `{"token":"`+token+`"}`)

	assert.Equal(t, http.StatusConflict, res.Code, res.Body.String())
	assert.Equal(t, "ann@example.com", s.currentEmail(access))
	assert.Zero(t, s.emailsTo("ann@example.com", "email_changed_notice"), "no change, no notice")
}

func TestChangeEmail_RequestsAreRateLimitedPerUser(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	access := s.loggedIn("ann@example.com")

	for _, to := range []string{"a@example.com", "b@example.com", "c@example.com"} {
		require.Equal(t, http.StatusOK, s.post(changeEmailPath, changeEmailBody(to, "correct-horse"), bearer(access)).Code)
	}

	limited := s.post(changeEmailPath, changeEmailBody("d@example.com", "correct-horse"), bearer(access))
	assert.Equal(t, http.StatusTooManyRequests, limited.Code)
	assert.Zero(t, s.emailsTo("d@example.com", "email_change_confirm"))
}

func TestChangeEmail_RequiresAuthentication(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")

	res := s.post(changeEmailPath, changeEmailBody("new@example.com", "correct-horse"))

	assert.Equal(t, http.StatusUnauthorized, res.Code)
}
