//go:build integration

package app_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (s *stack) orgCount() int {
	s.t.Helper()
	var n int
	require.NoError(s.t, s.queryRow("SELECT count(*) FROM organizations").Scan(&n))
	return n
}

// verifyByLink verifies an account the way the user would: with the token from
// the latest email sent to them.
func (s *stack) verifyByLink(email string) {
	s.t.Helper()
	res := s.post(verifyPath, `{"token":"`+s.latestEmailToken(email)+`"}`)
	require.Equal(s.t, http.StatusOK, res.Code, res.Body.String())
}

func TestRegisterWithAnInvitedEmail_JoinsTheInvitingOrganizationInsteadOfCreatingOne(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	id, _ := s.invite(tm, "newcomer@example.com", "admin")
	orgsBefore := s.orgCount()

	s.registered("newcomer@example.com")

	assert.Equal(t, orgsBefore, s.orgCount(), "no organization of their own: they are joining one")
	_, member := s.roleOf(tm.orgID, "newcomer@example.com")
	assert.False(t, member, "an address nobody has proven yet joins nothing")
	accepted, _, _ := s.invitationState(id)
	assert.False(t, accepted)

	s.verifyByLink("newcomer@example.com")

	role, member := s.roleOf(tm.orgID, "newcomer@example.com")
	require.True(t, member, "proving the address is what claims the invitation")
	assert.Equal(t, "admin", role)
	accepted, _, _ = s.invitationState(id)
	assert.True(t, accepted)
	me := s.get(mePath, bearer(s.loggedIn("newcomer@example.com"))).json()["organizations"].([]any)
	require.Len(t, me, 1, "exactly the inviting organization")
	assert.Equal(t, tm.orgID, me[0].(map[string]any)["id"])
	assert.Equal(t, orgsBefore, s.orgCount())
}

func TestRegisterWithAnInvitedEmail_JoinsEveryOrganizationThatInvitedIt(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	s.invite(tm, "newcomer@example.com", "member")
	s.verify("outsider@example.com")
	second := team{orgID: s.firstOrg("outsider@example.com"), owner: tm.outside}
	require.Equal(t, http.StatusCreated, s.post(second.invitationsPath(), inviteBody("newcomer@example.com", "admin"), bearer(second.owner)).Code)

	s.registered("newcomer@example.com")
	s.verifyByLink("newcomer@example.com")

	first, ok1 := s.roleOf(tm.orgID, "newcomer@example.com")
	other, ok2 := s.roleOf(second.orgID, "newcomer@example.com")
	assert.True(t, ok1 && ok2)
	assert.Equal(t, "member", first)
	assert.Equal(t, "admin", other)
}

func TestRegisterWithAnInvitedEmail_AnExpiredOrClosedInvitationDoesNotSkipTheirOwnOrganization(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	s.invite(tm, "late@example.com", "member")
	idRevoked, _ := s.invite(tm, "revoked@example.com", "member")
	require.Equal(t, http.StatusNoContent, s.do(http.MethodDelete, tm.invitationPath(idRevoked), "", bearer(tm.owner)).Code)
	_, declinedToken := s.invite(tm, "declined@example.com", "member")
	require.Equal(t, http.StatusNoContent, s.post(invitationLinkPath(declinedToken)+"/decline", ``).Code)
	s.clock.Advance(InvitationLifetime) // "late" has expired by now
	before := s.orgCount()

	for i, email := range []string{"late@example.com", "revoked@example.com", "declined@example.com"} {
		s.registered(email)

		assert.Equal(t, before+i+1, s.orgCount(), email+" gets an organization of their own")
	}
}

func TestVerifyingAnUninvitedAccountJoinsNothing(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	before := s.orgCount()

	s.verifyByLink("ann@example.com")

	assert.Equal(t, before, s.orgCount())
	var memberships int
	require.NoError(t, s.queryRow("SELECT count(*) FROM memberships").Scan(&memberships))
	assert.Equal(t, 1, memberships, "only her own owner membership")
}

func TestConfirmingAnInvitedNewEmailJoinsTheInvitingOrganization(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	s.invite(tm, "ann.new@example.com", "member")
	s.registered("ann@example.com")
	access := s.loggedIn("ann@example.com")
	require.Equal(t, http.StatusOK, s.post("/api/v1/me/email", changeEmailBody("ann.new@example.com", "correct-horse"), bearer(access)).Code)
	_, member := s.roleOf(tm.orgID, "ann@example.com")
	require.False(t, member, "requesting the change proves nothing yet")

	res := s.post("/api/v1/me/email/confirm", `{"token":"`+s.latestEmailToken("ann.new@example.com")+`"}`)

	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	role, member := s.roleOf(tm.orgID, "ann.new@example.com")
	assert.True(t, member, "the new address is proven, so the invitation sent to it is honored")
	assert.Equal(t, "member", role)
	assert.Equal(t, 2, len(s.get(orgsPath, bearer(s.loggedIn("ann.new@example.com"))).json()["items"].([]any)), "her own organization and the inviting one")
}

func TestVerificationFailureLeavesTheInvitationOpen(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	id, _ := s.invite(tm, "newcomer@example.com", "member")
	s.registered("newcomer@example.com")

	res := s.post(verifyPath, `{"token":"wrong"}`)

	assert.Equal(t, http.StatusBadRequest, res.Code)
	accepted, _, _ := s.invitationState(id)
	assert.False(t, accepted)
	s.clock.Advance(time.Minute)
	assert.Equal(t, 1, s.openInvitations(tm.orgID))
}
