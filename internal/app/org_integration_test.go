//go:build integration

package app_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const orgsPath = "/api/v1/orgs"

func orgPath(id string) string { return orgsPath + "/" + id }

// firstOrg returns the ID of the organization registration created for email.
func (s *stack) firstOrg(email string) string {
	s.t.Helper()
	var id string
	require.NoError(s.t, s.queryRow(`
		SELECT m.organization_id::text FROM memberships m JOIN users u ON u.id = m.user_id
		WHERE u.email = $1 ORDER BY m.created_at, m.id LIMIT 1`, email).Scan(&id))
	return id
}

// addMember inserts a membership directly (invitations have their own tests).
func (s *stack) addMember(orgID, email, role string) {
	s.t.Helper()
	_, err := s.admin.Exec(s.ctx(), `
		INSERT INTO memberships (organization_id, user_id, role)
		SELECT $1, id, $3 FROM users WHERE email = $2`, orgID, email, role)
	require.NoError(s.t, err)
}

// team registers an owner with an organization plus an admin and a member of it
// and returns everyone's access token.
type team struct {
	orgID                         string
	owner, admin, member, outside string // access tokens
}

func (s *stack) newTeam() team {
	s.t.Helper()
	for _, email := range []string{"owner@example.com", "admin@example.com", "member@example.com", "outsider@example.com"} {
		s.registered(email)
	}
	orgID := s.firstOrg("owner@example.com")
	s.addMember(orgID, "admin@example.com", "admin")
	s.addMember(orgID, "member@example.com", "member")
	return team{
		orgID:   orgID,
		owner:   s.loggedIn("owner@example.com"),
		admin:   s.loggedIn("admin@example.com"),
		member:  s.loggedIn("member@example.com"),
		outside: s.loggedIn("outsider@example.com"),
	}
}

func TestListOrgs_ShowsTheUsersOrganizationsWithTheirRole(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam()

	res := s.get(orgsPath, bearer(tm.admin))

	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	items := res.json()["items"].([]any)
	require.Len(t, items, 2, "the admin's own organization and the one they were added to")
	roles := map[string]string{}
	for _, it := range items {
		m := it.(map[string]any)
		roles[m["id"].(string)] = m["role"].(string)
	}
	assert.Equal(t, "admin", roles[tm.orgID])
	assert.Nil(t, res.json()["next_cursor"])
}

func TestListOrgs_PaginatesWithCursor(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	access := s.loggedIn("ann@example.com")
	for _, name := range []string{"Second", "Third"} {
		require.Equal(t, http.StatusCreated, s.post(orgsPath, `{"name":"`+name+`"}`, bearer(access)).Code)
		s.clock.Advance(1_000_000_000) // 1s: distinct membership timestamps
	}

	first := s.get(orgsPath+"?limit=2", bearer(access))
	require.Equal(t, http.StatusOK, first.Code)
	require.Len(t, first.json()["items"], 2)
	cursor, _ := first.json()["next_cursor"].(string)
	require.NotEmpty(t, cursor)

	second := s.get(orgsPath+"?limit=2&cursor="+cursor, bearer(access))

	require.Equal(t, http.StatusOK, second.Code, second.Body.String())
	require.Len(t, second.json()["items"], 1)
	assert.Nil(t, second.json()["next_cursor"])
	assert.Equal(t, http.StatusBadRequest, s.get(orgsPath+"?cursor=garbage", bearer(access)).Code)
}

func TestMe_ListsTheUsersOrganizations(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam()

	res := s.get(mePath, bearer(tm.member))

	require.Equal(t, http.StatusOK, res.Code)
	orgs := res.json()["organizations"].([]any)
	require.Len(t, orgs, 2)
	var found bool
	for _, o := range orgs {
		m := o.(map[string]any)
		if m["id"] == tm.orgID {
			found = true
			assert.Equal(t, "member", m["role"])
		}
	}
	assert.True(t, found)
}

func TestCreateOrg_MakesTheCallerTheOwner(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	access := s.loggedIn("ann@example.com")

	res := s.post(orgsPath, `{"name":"  Side Project "}`, bearer(access))

	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	created := res.json()["organization"].(map[string]any)
	assert.Equal(t, "Side Project", created["name"])
	assert.Equal(t, "side-project", created["slug"])
	assert.Equal(t, "owner", created["role"])
	assert.Equal(t, s.userID("ann@example.com"), created["owner_id"])
	assert.Equal(t, http.StatusOK, s.get(orgPath(created["id"].(string)), bearer(access)).Code)
}

func TestCreateOrg_Rejections(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	access := s.loggedIn("ann@example.com")
	require.Equal(t, http.StatusCreated, s.post(orgsPath, `{"name":"Taken","slug":"taken"}`, bearer(access)).Code)

	tests := map[string]struct {
		body string
		want int
	}{
		"blank name":           {`{"name":"   "}`, http.StatusBadRequest},
		"missing name":         {`{}`, http.StatusBadRequest},
		"invalid slug":         {`{"name":"X","slug":"Not A Slug"}`, http.StatusBadRequest},
		"slug already in use":  {`{"name":"Other","slug":"TAKEN"}`, http.StatusConflict},
		"unknown field (role)": {`{"name":"X","owner_id":"` + uuid.NewString() + `"}`, http.StatusBadRequest},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, s.post(orgsPath, tt.body, bearer(access)).Code)
		})
	}
	assert.Equal(t, http.StatusUnauthorized, s.post(orgsPath, `{"name":"X"}`).Code)
}

func TestGetOrg_AnyMemberMayViewIt(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam()

	for role, token := range map[string]string{"owner": tm.owner, "admin": tm.admin, "member": tm.member} {
		res := s.get(orgPath(tm.orgID), bearer(token))

		require.Equal(t, http.StatusOK, res.Code, role)
		got := res.json()["organization"].(map[string]any)
		assert.Equal(t, tm.orgID, got["id"])
		assert.Equal(t, role, got["role"], "the role is the caller's own")
	}
}

func TestGetOrg_NonMembersGet404NotForbidden(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam()

	for name, res := range map[string]response{
		"outsider":             s.get(orgPath(tm.orgID), bearer(tm.outside)),
		"unknown organization": s.get(orgPath(uuid.NewString()), bearer(tm.owner)),
		"malformed id":         s.get(orgPath("not-a-uuid"), bearer(tm.owner)),
	} {
		assert.Equal(t, http.StatusNotFound, res.Code, name)
		assert.Contains(t, res.Body.String(), `"code":"NOT_FOUND"`, name)
	}
	assert.Equal(t, http.StatusUnauthorized, s.get(orgPath(tm.orgID), nil).Code)
}

func TestUpdateOrg_OwnerAndAdminMayChangeSettingsButNotMembers(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam()

	byOwner := s.patch(orgPath(tm.orgID), `{"name":"Renamed","slug":"Renamed-Co","logo_url":"https://cdn.example.com/l.png"}`, bearer(tm.owner))
	require.Equal(t, http.StatusOK, byOwner.Code, byOwner.Body.String())
	got := byOwner.json()["organization"].(map[string]any)
	assert.Equal(t, "Renamed", got["name"])
	assert.Equal(t, "renamed-co", got["slug"])
	assert.Equal(t, "https://cdn.example.com/l.png", got["logo_url"])

	byAdmin := s.patch(orgPath(tm.orgID), `{"name":"By Admin"}`, bearer(tm.admin))
	require.Equal(t, http.StatusOK, byAdmin.Code, byAdmin.Body.String())
	assert.Equal(t, "renamed-co", byAdmin.json()["organization"].(map[string]any)["slug"], "untouched fields stay")

	byMember := s.patch(orgPath(tm.orgID), `{"name":"By Member"}`, bearer(tm.member))
	assert.Equal(t, http.StatusForbidden, byMember.Code)
	byOutsider := s.patch(orgPath(tm.orgID), `{"name":"By Outsider"}`, bearer(tm.outside))
	assert.Equal(t, http.StatusNotFound, byOutsider.Code)

	assert.Equal(t, "By Admin", s.get(orgPath(tm.orgID), bearer(tm.owner)).json()["organization"].(map[string]any)["name"])
}

func TestUpdateOrg_NullLogoClearsItAndBadInputIsRejected(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam()
	require.Equal(t, http.StatusOK, s.patch(orgPath(tm.orgID), `{"logo_url":"https://cdn.example.com/l.png"}`, bearer(tm.owner)).Code)

	cleared := s.patch(orgPath(tm.orgID), `{"logo_url":null}`, bearer(tm.owner))
	require.Equal(t, http.StatusOK, cleared.Code, cleared.Body.String())
	assert.Nil(t, cleared.json()["organization"].(map[string]any)["logo_url"])

	for name, body := range map[string]string{
		"empty object":    `{}`,
		"blank name":      `{"name":" "}`,
		"bad slug":        `{"slug":"x"}`,
		"javascript logo": `{"logo_url":"javascript:alert(1)"}`,
		"owner_id":        `{"owner_id":"` + uuid.NewString() + `"}`,
	} {
		assert.Equal(t, http.StatusBadRequest, s.patch(orgPath(tm.orgID), body, bearer(tm.owner)).Code, name)
	}
}

func TestUpdateOrg_ASlugInUseByAnotherOrganizationIsAConflict(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam()
	require.Equal(t, http.StatusCreated, s.post(orgsPath, `{"name":"Other","slug":"other-co"}`, bearer(tm.owner)).Code)

	res := s.patch(orgPath(tm.orgID), `{"slug":"other-co"}`, bearer(tm.owner))

	assert.Equal(t, http.StatusConflict, res.Code)
}

func TestDeleteOrg_OnlyTheOwnerMayAndItEndsEveryonesAccess(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam()
	_, err := s.admin.Exec(s.ctx(), `
		INSERT INTO invitations (organization_id, email, role, token_hash, invited_by, expires_at)
		SELECT $1, 'pending@example.com', 'member', 'hash', id, now() + interval '7 days' FROM users WHERE email = 'owner@example.com'`, tm.orgID)
	require.NoError(t, err)

	assert.Equal(t, http.StatusForbidden, s.do(http.MethodDelete, orgPath(tm.orgID), "", bearer(tm.admin)).Code)
	assert.Equal(t, http.StatusForbidden, s.do(http.MethodDelete, orgPath(tm.orgID), "", bearer(tm.member)).Code)
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodDelete, orgPath(tm.orgID), "", bearer(tm.outside)).Code)

	res := s.do(http.MethodDelete, orgPath(tm.orgID), "", bearer(tm.owner))
	require.Equal(t, http.StatusNoContent, res.Code, res.Body.String())

	for name, token := range map[string]string{"owner": tm.owner, "admin": tm.admin, "member": tm.member} {
		assert.Equal(t, http.StatusNotFound, s.get(orgPath(tm.orgID), bearer(token)).Code, name+" lost access")
	}
	var open int
	require.NoError(t, s.queryRow(`SELECT count(*) FROM invitations WHERE organization_id = $1 AND revoked_at IS NULL`, tm.orgID).Scan(&open))
	assert.Zero(t, open, "its open invitations were revoked")
	for _, it := range s.get(orgsPath, bearer(tm.member)).json()["items"].([]any) {
		assert.NotEqual(t, tm.orgID, it.(map[string]any)["id"], "no longer listed")
	}
	var slugKept int
	require.NoError(t, s.queryRow(`SELECT count(*) FROM organizations WHERE id = $1 AND deleted_at IS NOT NULL`, tm.orgID).Scan(&slugKept))
	assert.Equal(t, 1, slugKept, "soft delete: the row and its slug are kept")
}
