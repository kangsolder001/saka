//go:build integration

package app_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (tm team) invitationsPath() string { return orgPath(tm.orgID) + "/invitations" }

func (tm team) invitationPath(id string) string { return tm.invitationsPath() + "/" + id }

func inviteBody(email, role string) string {
	return `{"email":"` + email + `","role":"` + role + `"}`
}

// verify marks the accounts' emails as verified and drops their cached
// summaries, the way the verification flow would.
func (s *stack) verify(emails ...string) {
	s.t.Helper()
	for _, email := range emails {
		_, err := s.admin.Exec(context.Background(), "UPDATE users SET email_verified_at = now() WHERE email = $1", email)
		require.NoError(s.t, err)
	}
	s.invalidateAllUsers()
}

// verifiedTeam is a team whose owner and admin have verified their email. It is
// subscribed to Pro: its three members already fill Free's seats, and the
// invitation tests are about invitations, not about the plan (see
// entitlement_integration_test.go for that).
func (s *stack) verifiedTeam() team {
	s.t.Helper()
	tm := s.newTeam()
	s.verify("owner@example.com", "admin@example.com")
	s.setPlan(tm.orgID, "pro", "active")
	return tm
}

// openInvitations counts the organization's invitations that are still open.
func (s *stack) openInvitations(orgID string) int {
	s.t.Helper()
	var n int
	require.NoError(s.t, s.queryRow(`SELECT count(*) FROM invitations WHERE organization_id = $1
		AND accepted_at IS NULL AND declined_at IS NULL AND revoked_at IS NULL`, orgID).Scan(&n))
	return n
}

func (s *stack) invitationTokenHash(id string) string {
	s.t.Helper()
	var hash string
	require.NoError(s.t, s.queryRow("SELECT token_hash FROM invitations WHERE id = $1", id).Scan(&hash))
	return hash
}

func TestInvite_CreatesAnInvitationAndQueuesTheEmailWithItsLink(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()

	res := s.post(tm.invitationsPath(), inviteBody("New.Person@Example.com", "admin"), bearer(tm.owner))

	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	inv := res.json()["invitation"].(map[string]any)
	assert.Equal(t, "new.person@example.com", inv["email"])
	assert.Equal(t, "admin", inv["role"])
	assert.Equal(t, s.userID("owner@example.com"), inv["invited_by"])
	assert.NotContains(t, res.Body.String(), "token", "the token only ever travels by email")
	expires, err := time.Parse(time.RFC3339, inv["expires_at"].(string))
	require.NoError(t, err)
	assert.True(t, expires.Equal(epoch.Add(7*24*time.Hour)), "valid for 7 days")

	require.Equal(t, 1, s.emailsTo("new.person@example.com", "invitation"))
	token := s.latestEmailToken("new.person@example.com")
	assert.Equal(t, authHash(token), s.invitationTokenHash(inv["id"].(string)), "the emailed token is the one whose hash is stored")
}

func TestInvite_InvitingAnAddressWithAnOpenInvitationRefreshesIt(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	first := s.post(tm.invitationsPath(), inviteBody("guest@example.com", "member"), bearer(tm.owner))
	require.Equal(t, http.StatusCreated, first.Code)
	id := first.json()["invitation"].(map[string]any)["id"].(string)
	oldToken := s.latestEmailToken("guest@example.com")
	s.clock.Advance(24 * time.Hour)
	tm.admin = s.loggedIn("admin@example.com") // the access tokens above lasted 15 minutes

	second := s.post(tm.invitationsPath(), inviteBody("GUEST@example.com", "admin"), bearer(tm.admin))

	require.Equal(t, http.StatusOK, second.Code, second.Body.String())
	refreshed := second.json()["invitation"].(map[string]any)
	assert.Equal(t, id, refreshed["id"], "the same invitation, not a second one")
	assert.Equal(t, "admin", refreshed["role"])
	assert.Equal(t, s.userID("admin@example.com"), refreshed["invited_by"])
	assert.Equal(t, 1, s.openInvitations(tm.orgID))
	assert.Equal(t, 2, s.emailsTo("guest@example.com", "invitation"))
	newToken := s.latestEmailToken("guest@example.com")
	assert.NotEqual(t, oldToken, newToken)
	assert.Equal(t, authHash(newToken), s.invitationTokenHash(id), "only the newest link works")
	expires, err := time.Parse(time.RFC3339, refreshed["expires_at"].(string))
	require.NoError(t, err)
	assert.True(t, expires.Equal(epoch.Add(24*time.Hour+7*24*time.Hour)), "the 7 days restart")
}

func TestInvite_ExistingMembersCannotBeInvited(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()

	for _, email := range []string{"member@example.com", "OWNER@example.com", "admin@example.com"} {
		res := s.post(tm.invitationsPath(), inviteBody(email, "member"), bearer(tm.owner))

		assert.Equal(t, http.StatusConflict, res.Code, email)
	}
	assert.Zero(t, s.openInvitations(tm.orgID))
	assert.Equal(t, http.StatusCreated, s.post(tm.invitationsPath(), inviteBody("outsider@example.com", "member"), bearer(tm.owner)).Code,
		"a registered user who is not a member of this organization can be invited")
}

func TestInvite_FollowsThePermissionMatrix(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	s.verify("member@example.com", "outsider@example.com")

	assert.Equal(t, http.StatusCreated, s.post(tm.invitationsPath(), inviteBody("a@example.com", "member"), bearer(tm.owner)).Code)
	assert.Equal(t, http.StatusCreated, s.post(tm.invitationsPath(), inviteBody("b@example.com", "member"), bearer(tm.admin)).Code)
	assert.Equal(t, http.StatusForbidden, s.post(tm.invitationsPath(), inviteBody("c@example.com", "member"), bearer(tm.member)).Code)
	assert.Equal(t, http.StatusNotFound, s.post(tm.invitationsPath(), inviteBody("d@example.com", "member"), bearer(tm.outside)).Code)
	assert.Equal(t, http.StatusUnauthorized, s.post(tm.invitationsPath(), inviteBody("e@example.com", "member")).Code)
	assert.Equal(t, 2, s.openInvitations(tm.orgID))
}

func TestInvite_RequiresAVerifiedEmail(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam() // nobody verified

	res := s.post(tm.invitationsPath(), inviteBody("guest@example.com", "member"), bearer(tm.owner))

	assert.Equal(t, http.StatusForbidden, res.Code)
	assert.Contains(t, res.Body.String(), `"code":"EMAIL_NOT_VERIFIED"`)
	assert.Zero(t, s.openInvitations(tm.orgID))
	assert.Zero(t, s.emailsTo("guest@example.com", "invitation"))
}

func TestInvite_RejectsInvalidInput(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()

	for name, body := range map[string]string{
		"missing email": `{"role":"member"}`,
		"malformed":     inviteBody("not-an-email", "member"),
		"owner role":    inviteBody("a@example.com", "owner"),
		"unknown role":  inviteBody("a@example.com", "root"),
		"missing role":  `{"email":"a@example.com"}`,
		"unknown field": `{"email":"a@example.com","role":"member","expires_in":1}`,
	} {
		assert.Equal(t, http.StatusBadRequest, s.post(tm.invitationsPath(), body, bearer(tm.owner)).Code, name)
	}
	assert.Zero(t, s.openInvitations(tm.orgID))
}

func TestInvite_IsAtomicWithItsEmail(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	_, err := s.admin.Exec(context.Background(), "DROP TABLE outbox_jobs")
	require.NoError(t, err)

	res := s.post(tm.invitationsPath(), inviteBody("guest@example.com", "member"), bearer(tm.owner))

	assert.Equal(t, http.StatusInternalServerError, res.Code)
	assert.Zero(t, s.openInvitations(tm.orgID), "no invitation without its email")
}

func TestListInvitations_ShowsOnlyActiveOnesToInviters(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	ids := map[string]string{}
	for _, email := range []string{"keep@example.com", "revoked@example.com", "old@example.com"} {
		res := s.post(tm.invitationsPath(), inviteBody(email, "member"), bearer(tm.owner))
		require.Equal(t, http.StatusCreated, res.Code)
		ids[email] = res.json()["invitation"].(map[string]any)["id"].(string)
		s.clock.Advance(time.Second)
	}
	require.Equal(t, http.StatusNoContent, s.do(http.MethodDelete, tm.invitationPath(ids["revoked@example.com"]), "", bearer(tm.owner)).Code)
	s.clock.Advance(7*24*time.Hour - time.Second) // "old" and "revoked" age out; only a fresh one is active
	tm.owner, tm.admin = s.loggedIn("owner@example.com"), s.loggedIn("admin@example.com")
	tm.member, tm.outside = s.loggedIn("member@example.com"), s.loggedIn("outsider@example.com")
	require.Equal(t, http.StatusCreated, s.post(tm.invitationsPath(), inviteBody("fresh@example.com", "admin"), bearer(tm.admin)).Code)

	res := s.get(tm.invitationsPath(), bearer(tm.admin))

	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	items := res.json()["items"].([]any)
	require.Len(t, items, 1)
	assert.Equal(t, "fresh@example.com", items[0].(map[string]any)["email"])
	assert.Equal(t, http.StatusForbidden, s.get(tm.invitationsPath(), bearer(tm.member)).Code)
	assert.Equal(t, http.StatusNotFound, s.get(tm.invitationsPath(), bearer(tm.outside)).Code)
}

func TestListInvitations_Paginates(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	for _, email := range []string{"a@example.com", "b@example.com", "c@example.com"} {
		require.Equal(t, http.StatusCreated, s.post(tm.invitationsPath(), inviteBody(email, "member"), bearer(tm.owner)).Code)
		s.clock.Advance(time.Second)
	}

	first := s.get(tm.invitationsPath()+"?limit=2", bearer(tm.owner))
	require.Len(t, first.json()["items"], 2)
	cursor, _ := first.json()["next_cursor"].(string)
	require.NotEmpty(t, cursor)
	second := s.get(tm.invitationsPath()+"?limit=2&cursor="+cursor, bearer(tm.owner))

	require.Len(t, second.json()["items"], 1)
	assert.Equal(t, "c@example.com", second.json()["items"].([]any)[0].(map[string]any)["email"])
	assert.Nil(t, second.json()["next_cursor"])
}

func TestRevokeInvitation_KillsTheLinkAndOnlyOnce(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	id := s.post(tm.invitationsPath(), inviteBody("guest@example.com", "member"), bearer(tm.owner)).json()["invitation"].(map[string]any)["id"].(string)

	assert.Equal(t, http.StatusForbidden, s.do(http.MethodDelete, tm.invitationPath(id), "", bearer(tm.member)).Code)
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodDelete, tm.invitationPath(id), "", bearer(tm.outside)).Code)
	require.Equal(t, http.StatusNoContent, s.do(http.MethodDelete, tm.invitationPath(id), "", bearer(tm.admin)).Code)

	assert.Zero(t, s.openInvitations(tm.orgID))
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodDelete, tm.invitationPath(id), "", bearer(tm.owner)).Code, "already cancelled")
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodDelete, tm.invitationPath("not-a-uuid"), "", bearer(tm.owner)).Code)
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodDelete, tm.invitationPath(uuid.NewString()), "", bearer(tm.owner)).Code)
}

func TestInvitations_OfAnotherOrganizationAreUntouchable(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	s.verify("outsider@example.com")
	outsiderOrg := s.firstOrg("outsider@example.com")
	other := team{orgID: outsiderOrg}
	id := s.post(other.invitationsPath(), inviteBody("guest@example.com", "member"), bearer(tm.outside)).json()["invitation"].(map[string]any)["id"].(string)

	// The owner of org A names org B's invitation inside org A's URL: not found, and still open.
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodDelete, tm.invitationPath(id), "", bearer(tm.owner)).Code)
	assert.Equal(t, http.StatusNotFound, s.post(tm.invitationPath(id)+"/resend", ``, bearer(tm.owner)).Code)
	assert.Equal(t, 1, s.openInvitations(outsiderOrg))
	// And org A's owner cannot reach org B's list at all.
	assert.Equal(t, http.StatusNotFound, s.get(other.invitationsPath(), bearer(tm.owner)).Code)
}

func TestResendInvitation_IssuesANewLinkAndKillsTheOldOne(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	id := s.post(tm.invitationsPath(), inviteBody("guest@example.com", "member"), bearer(tm.owner)).json()["invitation"].(map[string]any)["id"].(string)
	oldToken := s.latestEmailToken("guest@example.com")
	s.clock.Advance(8 * 24 * time.Hour) // expired by now
	tm.admin = s.loggedIn("admin@example.com")

	res := s.post(tm.invitationPath(id)+"/resend", ``, bearer(tm.admin))

	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	assert.Equal(t, 2, s.emailsTo("guest@example.com", "invitation"))
	newToken := s.latestEmailToken("guest@example.com")
	assert.NotEqual(t, oldToken, newToken)
	assert.Equal(t, authHash(newToken), s.invitationTokenHash(id))
	expires, err := time.Parse(time.RFC3339, res.json()["invitation"].(map[string]any)["expires_at"].(string))
	require.NoError(t, err)
	assert.True(t, expires.After(s.clock.Now().Add(6*24*time.Hour)), "an expired invitation is revived")
	assert.Equal(t, 1, s.openInvitations(tm.orgID))
}

func TestResendInvitation_Rejections(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	id := s.post(tm.invitationsPath(), inviteBody("guest@example.com", "member"), bearer(tm.owner)).json()["invitation"].(map[string]any)["id"].(string)
	s.verify("member@example.com")

	assert.Equal(t, http.StatusForbidden, s.post(tm.invitationPath(id)+"/resend", ``, bearer(tm.member)).Code)
	assert.Equal(t, http.StatusNotFound, s.post(tm.invitationPath(id)+"/resend", ``, bearer(tm.outside)).Code)
	assert.Equal(t, http.StatusNotFound, s.post(tm.invitationPath(uuid.NewString())+"/resend", ``, bearer(tm.owner)).Code)
	require.Equal(t, http.StatusNoContent, s.do(http.MethodDelete, tm.invitationPath(id), "", bearer(tm.owner)).Code)
	assert.Equal(t, http.StatusNotFound, s.post(tm.invitationPath(id)+"/resend", ``, bearer(tm.owner)).Code, "a cancelled invitation stays cancelled")
	assert.Equal(t, 1, s.emailsTo("guest@example.com", "invitation"), "nothing was sent for the refused attempts")
}

func TestResendInvitation_RequiresAVerifiedEmail(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	id := s.post(tm.invitationsPath(), inviteBody("guest@example.com", "member"), bearer(tm.owner)).json()["invitation"].(map[string]any)["id"].(string)
	_, err := s.admin.Exec(context.Background(), "UPDATE users SET email_verified_at = NULL WHERE email = 'owner@example.com'")
	require.NoError(t, err)
	s.invalidateAllUsers()

	res := s.post(tm.invitationPath(id)+"/resend", ``, bearer(tm.owner))

	assert.Equal(t, http.StatusForbidden, res.Code)
	assert.Contains(t, res.Body.String(), "EMAIL_NOT_VERIFIED")
}
