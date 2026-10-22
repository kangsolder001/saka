//go:build integration

package app_test

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func invitationLinkPath(token string) string { return "/api/v1/invitations/" + token }

// invite sends an invitation as the owner and returns the raw token from the
// email, like the link the invitee would click.
func (s *stack) invite(tm team, email, role string) (id, token string) {
	s.t.Helper()
	res := s.post(tm.invitationsPath(), inviteBody(email, role), bearer(tm.owner))
	require.Equal(s.t, http.StatusCreated, res.Code, res.Body.String())
	return res.json()["invitation"].(map[string]any)["id"].(string), s.latestEmailToken(email)
}

func reasonOf(res response) any {
	return res.json()["error"].(map[string]any)["details"].(map[string]any)["reason"]
}

func (s *stack) invitationState(id string) (accepted, declined, revoked bool) {
	s.t.Helper()
	require.NoError(s.t, s.queryRow(`SELECT accepted_at IS NOT NULL, declined_at IS NOT NULL, revoked_at IS NOT NULL
		FROM invitations WHERE id = $1`, id).Scan(&accepted, &declined, &revoked))
	return accepted, declined, revoked
}

func TestInvitationInfo_IsPublicAndSaysWhatIsOnOffer(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	_, existing := s.invite(tm, "outsider@example.com", "admin")
	_, brandNew := s.invite(tm, "nobody@example.com", "member")

	res := s.get(invitationLinkPath(existing), nil)

	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	inv := res.json()["invitation"].(map[string]any)
	assert.Equal(t, "outsider@example.com", inv["email"])
	assert.Equal(t, "admin", inv["role"])
	assert.Equal(t, "Ann", inv["inviter_name"], "the inviter's display name")
	assert.Equal(t, "Acme", inv["organization"].(map[string]any)["name"])
	assert.Equal(t, true, inv["account_exists"], "log in")
	assert.Equal(t, false, s.get(invitationLinkPath(brandNew), nil).json()["invitation"].(map[string]any)["account_exists"], "register first")
	assert.NotContains(t, res.Body.String(), existing, "the token is not echoed")
	assert.NotContains(t, res.Body.String(), s.userID("owner@example.com"), "no internal ids of the inviter")
}

func TestInvitationInfo_UnusableLinksAreNotFoundWithAReason(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	_, expired := s.invite(tm, "a@example.com", "member")
	idRevoked, revoked := s.invite(tm, "b@example.com", "member")
	require.Equal(t, http.StatusNoContent, s.do(http.MethodDelete, tm.invitationPath(idRevoked), "", bearer(tm.owner)).Code)
	_, declined := s.invite(tm, "c@example.com", "member")
	require.Equal(t, http.StatusNoContent, s.post(invitationLinkPath(declined)+"/decline", ``).Code)

	unknown := s.get(invitationLinkPath("no-such-token"), nil)
	assert.Equal(t, http.StatusNotFound, unknown.Code)
	assert.Empty(t, unknown.json()["error"].(map[string]any)["details"], "an unknown token reveals nothing")
	assert.Equal(t, "revoked", reasonOf(s.get(invitationLinkPath(revoked), nil)))
	assert.Equal(t, "declined", reasonOf(s.get(invitationLinkPath(declined), nil)))
	assert.Equal(t, http.StatusOK, s.get(invitationLinkPath(expired), nil).Code)

	s.clock.Advance(InvitationLifetime)
	assert.Equal(t, "expired", reasonOf(s.get(invitationLinkPath(expired), nil)), "invitations expire after 7 days")
}

// InvitationLifetime is spec §4.6.
const InvitationLifetime = 7 * 24 * time.Hour

func TestAcceptInvitation_JoinsWithTheInvitedRoleOnce(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	s.verify("outsider@example.com")
	id, token := s.invite(tm, "outsider@example.com", "admin")
	assert.Equal(t, http.StatusNotFound, s.get(orgPath(tm.orgID), bearer(tm.outside)).Code, "not a member yet")

	res := s.post(invitationLinkPath(token)+"/accept", ``, bearer(tm.outside))

	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	joined := res.json()["organization"].(map[string]any)
	assert.Equal(t, tm.orgID, joined["id"])
	assert.Equal(t, "admin", joined["role"])
	role, member := s.roleOf(tm.orgID, "outsider@example.com")
	assert.True(t, member)
	assert.Equal(t, "admin", role)
	accepted, _, _ := s.invitationState(id)
	assert.True(t, accepted)
	assert.Equal(t, http.StatusOK, s.get(orgPath(tm.orgID), bearer(tm.outside)).Code, "access starts at once")
	assert.Zero(t, s.openInvitations(tm.orgID))

	again := s.post(invitationLinkPath(token)+"/accept", ``, bearer(tm.outside))
	assert.Equal(t, http.StatusNotFound, again.Code)
	assert.Equal(t, "accepted", reasonOf(again), "a link works once")
	assert.Equal(t, http.StatusNotFound, s.get(invitationLinkPath(token), nil).Code)
}

func TestAcceptInvitation_OnlyTheInvitedEmailMay(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	s.verify("outsider@example.com")
	id, token := s.invite(tm, "outsider@example.com", "member")
	s.registered("third@example.com")
	s.verify("third@example.com")
	thirdToken := s.loggedIn("third@example.com")

	res := s.post(invitationLinkPath(token)+"/accept", ``, bearer(thirdToken))

	require.Equal(t, http.StatusForbidden, res.Code, res.Body.String())
	assert.Equal(t, "invitation_email_mismatch", reasonOf(res))
	_, member := s.roleOf(tm.orgID, "third@example.com")
	assert.False(t, member)
	accepted, _, _ := s.invitationState(id)
	assert.False(t, accepted, "someone else's attempt does not burn the invitation")
	assert.Equal(t, http.StatusOK, s.post(invitationLinkPath(token)+"/accept", ``, bearer(tm.outside)).Code, "the real invitee still can")
}

func TestAcceptInvitation_NeedsALoginWithAVerifiedEmail(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	_, token := s.invite(tm, "outsider@example.com", "member")

	noLogin := s.post(invitationLinkPath(token)+"/accept", ``)
	unverified := s.post(invitationLinkPath(token)+"/accept", ``, bearer(tm.outside)) // outsider never verified

	assert.Equal(t, http.StatusUnauthorized, noLogin.Code)
	assert.Equal(t, http.StatusForbidden, unverified.Code)
	assert.Contains(t, unverified.Body.String(), "EMAIL_NOT_VERIFIED")
	_, member := s.roleOf(tm.orgID, "outsider@example.com")
	assert.False(t, member, "an address that was never proven cannot claim an invitation")
}

func TestAcceptInvitation_ExpiryIsExactlySevenDays(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	s.verify("outsider@example.com")
	_, early := s.invite(tm, "outsider@example.com", "member")
	s.clock.Advance(InvitationLifetime - time.Second)
	assert.Equal(t, http.StatusOK, s.get(invitationLinkPath(early), nil).Code, "one second left")

	s.clock.Advance(time.Second)

	res := s.post(invitationLinkPath(early)+"/accept", ``, bearer(s.loggedIn("outsider@example.com")))
	assert.Equal(t, http.StatusNotFound, res.Code)
	assert.Equal(t, "expired", reasonOf(res))
	_, member := s.roleOf(tm.orgID, "outsider@example.com")
	assert.False(t, member)
}

func TestAcceptInvitation_RevokedLinksAreDead(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	s.verify("outsider@example.com")
	id, token := s.invite(tm, "outsider@example.com", "member")
	require.Equal(t, http.StatusNoContent, s.do(http.MethodDelete, tm.invitationPath(id), "", bearer(tm.owner)).Code)

	res := s.post(invitationLinkPath(token)+"/accept", ``, bearer(tm.outside))

	assert.Equal(t, http.StatusNotFound, res.Code)
	assert.Equal(t, "revoked", reasonOf(res))
}

func TestAcceptInvitation_RefreshedInvitationOnlyHonorsTheNewLink(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	s.verify("outsider@example.com")
	_, oldToken := s.invite(tm, "outsider@example.com", "member")
	require.Equal(t, http.StatusOK, s.post(tm.invitationsPath(), inviteBody("outsider@example.com", "admin"), bearer(tm.owner)).Code)
	newToken := s.latestEmailToken("outsider@example.com")

	assert.Equal(t, http.StatusNotFound, s.post(invitationLinkPath(oldToken)+"/accept", ``, bearer(tm.outside)).Code)
	res := s.post(invitationLinkPath(newToken)+"/accept", ``, bearer(tm.outside))

	require.Equal(t, http.StatusOK, res.Code)
	assert.Equal(t, "admin", res.json()["organization"].(map[string]any)["role"], "the refreshed role applies")
}

func TestAcceptInvitation_AlreadyAMemberKeepsTheirRoleAndClosesTheInvitation(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	s.verify("outsider@example.com")
	id, token := s.invite(tm, "outsider@example.com", "admin")
	s.addMember(tm.orgID, "outsider@example.com", "member") // joined some other way meanwhile

	res := s.post(invitationLinkPath(token)+"/accept", ``, bearer(tm.outside))

	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	role, _ := s.roleOf(tm.orgID, "outsider@example.com")
	assert.Equal(t, "member", role, "an invitation never changes an existing member's role")
	accepted, _, _ := s.invitationState(id)
	assert.True(t, accepted)
}

func TestAcceptInvitation_ConcurrentUsesOfOneLinkJoinOnce(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	s.verify("outsider@example.com")
	_, token := s.invite(tm, "outsider@example.com", "member")

	codes := make([]int, 6)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = s.post(invitationLinkPath(token)+"/accept", ``, bearer(tm.outside)).Code
		}()
	}
	wg.Wait()

	ok := 0
	for _, c := range codes {
		if c == http.StatusOK {
			ok++
		} else {
			assert.Equal(t, http.StatusNotFound, c)
		}
	}
	assert.Equal(t, 1, ok, "exactly one request wins the race")
	var memberships int
	require.NoError(t, s.queryRow(`SELECT count(*) FROM memberships m JOIN users u ON u.id = m.user_id
		WHERE m.organization_id = $1 AND u.email = 'outsider@example.com'`, tm.orgID).Scan(&memberships))
	assert.Equal(t, 1, memberships)
}

func TestDeclineInvitation_NeedsNoLoginAndClosesTheLink(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	s.verify("outsider@example.com")
	id, token := s.invite(tm, "outsider@example.com", "member")

	res := s.post(invitationLinkPath(token)+"/decline", ``)

	require.Equal(t, http.StatusNoContent, res.Code, res.Body.String())
	_, declined, _ := s.invitationState(id)
	assert.True(t, declined)
	_, member := s.roleOf(tm.orgID, "outsider@example.com")
	assert.False(t, member)
	accept := s.post(invitationLinkPath(token)+"/accept", ``, bearer(tm.outside))
	assert.Equal(t, http.StatusNotFound, accept.Code)
	assert.Equal(t, "declined", reasonOf(accept))
	assert.Equal(t, http.StatusNotFound, s.post(invitationLinkPath(token)+"/decline", ``).Code, "declining twice")
	assert.Equal(t, http.StatusNotFound, s.post(invitationLinkPath("no-such-token")+"/decline", ``).Code)
	assert.Zero(t, s.openInvitations(tm.orgID))
}

func TestDeclinedInvitationCanBeIssuedAgain(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	_, token := s.invite(tm, "outsider@example.com", "member")
	require.Equal(t, http.StatusNoContent, s.post(invitationLinkPath(token)+"/decline", ``).Code)

	res := s.post(tm.invitationsPath(), inviteBody("outsider@example.com", "member"), bearer(tm.owner))

	assert.Equal(t, http.StatusCreated, res.Code, "a closed invitation does not block a new one")
	assert.Equal(t, 1, s.openInvitations(tm.orgID))
}
