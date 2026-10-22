//go:build integration

package app_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (tm team) membersPath() string { return orgPath(tm.orgID) + "/members" }

func (tm team) memberPath(userID string) string { return tm.membersPath() + "/" + userID }

func roleBody(role string) string { return `{"role":"` + role + `"}` }

// roleOf reads the stored role directly, bypassing the API.
func (s *stack) roleOf(orgID, email string) (string, bool) {
	s.t.Helper()
	var role string
	err := s.queryRow(`SELECT m.role FROM memberships m JOIN users u ON u.id = m.user_id
		WHERE m.organization_id = $1 AND u.email = $2`, orgID, email).Scan(&role)
	return role, err == nil
}

func TestListMembers_AnyMemberSeesEveryoneWithTheirRoleOwnerFirst(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam()

	for name, token := range map[string]string{"owner": tm.owner, "admin": tm.admin, "member": tm.member} {
		res := s.get(tm.membersPath(), bearer(token))

		require.Equal(t, http.StatusOK, res.Code, name)
		items := res.json()["items"].([]any)
		require.Len(t, items, 3, name)
		first := items[0].(map[string]any)
		assert.Equal(t, "owner@example.com", first["email"])
		assert.Equal(t, "owner", first["role"])
		assert.NotContains(t, res.Body.String(), "password")
		assert.NotContains(t, res.Body.String(), "argon2id")
	}
}

func TestListMembers_PaginatesAndHidesTheOrganizationFromOutsiders(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam()

	first := s.get(tm.membersPath()+"?limit=2", bearer(tm.owner))
	require.Equal(t, http.StatusOK, first.Code)
	require.Len(t, first.json()["items"], 2)
	cursor, _ := first.json()["next_cursor"].(string)
	require.NotEmpty(t, cursor)
	second := s.get(tm.membersPath()+"?limit=2&cursor="+cursor, bearer(tm.owner))
	require.Len(t, second.json()["items"], 1)
	assert.Nil(t, second.json()["next_cursor"])

	assert.Equal(t, http.StatusNotFound, s.get(tm.membersPath(), bearer(tm.outside)).Code)
	assert.Equal(t, http.StatusUnauthorized, s.get(tm.membersPath(), nil).Code)
}

func TestChangeRole_FollowsThePermissionMatrix(t *testing.T) {
	type attempt struct {
		name   string
		actor  func(team) string
		target string // email
		role   string
		want   int
	}
	tests := []attempt{
		{"owner promotes a member", func(tm team) string { return tm.owner }, "member@example.com", "admin", http.StatusOK},
		{"owner demotes an admin", func(tm team) string { return tm.owner }, "admin@example.com", "member", http.StatusOK},
		{"owner cannot change the owner", func(tm team) string { return tm.owner }, "owner@example.com", "admin", http.StatusForbidden},
		{"admin promotes a member", func(tm team) string { return tm.admin }, "member@example.com", "admin", http.StatusOK},
		{"admin cannot demote another admin", func(tm team) string { return tm.admin }, "admin@example.com", "member", http.StatusForbidden},
		{"admin cannot touch the owner", func(tm team) string { return tm.admin }, "owner@example.com", "member", http.StatusForbidden},
		{"member cannot change roles", func(tm team) string { return tm.member }, "member@example.com", "admin", http.StatusForbidden},
		{"owner cannot hand out the owner role", func(tm team) string { return tm.owner }, "member@example.com", "owner", http.StatusBadRequest},
		{"unknown role is invalid", func(tm team) string { return tm.owner }, "member@example.com", "root", http.StatusBadRequest},
		{"outsiders get 404", func(tm team) string { return tm.outside }, "member@example.com", "admin", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newStack(t)
			tm := s.newTeam()
			before, _ := s.roleOf(tm.orgID, tt.target)

			res := s.patch(tm.memberPath(s.userID(tt.target)), roleBody(tt.role), bearer(tt.actor(tm)))

			require.Equal(t, tt.want, res.Code, res.Body.String())
			after, _ := s.roleOf(tm.orgID, tt.target)
			if tt.want == http.StatusOK {
				assert.Equal(t, tt.role, after)
				assert.Equal(t, tt.role, res.json()["member"].(map[string]any)["role"])
			} else {
				assert.Equal(t, before, after, "a refused change leaves the role alone")
			}
		})
	}
}

func TestChangeRole_UnknownAndMalformedTargetsAre404(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam()

	assert.Equal(t, http.StatusNotFound, s.patch(tm.memberPath(uuid.NewString()), roleBody("admin"), bearer(tm.owner)).Code)
	assert.Equal(t, http.StatusNotFound, s.patch(tm.memberPath("nope"), roleBody("admin"), bearer(tm.owner)).Code)
	assert.Equal(t, http.StatusNotFound, s.patch(tm.memberPath(s.userID("outsider@example.com")), roleBody("admin"), bearer(tm.owner)).Code,
		"a user of another organization is not a member here")
}

func TestChangeRole_TakesEffectOnTheNextRequestWithoutANewLogin(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam()
	require.Equal(t, http.StatusForbidden, s.patch(orgPath(tm.orgID), `{"name":"X"}`, bearer(tm.member)).Code)

	require.Equal(t, http.StatusOK, s.patch(tm.memberPath(s.userID("member@example.com")), roleBody("admin"), bearer(tm.owner)).Code)

	assert.Equal(t, http.StatusOK, s.patch(orgPath(tm.orgID), `{"name":"X"}`, bearer(tm.member)).Code,
		"roles are read from the database per request, not carried in the token")
}

func TestRemoveMember_FollowsThePermissionMatrix(t *testing.T) {
	tests := []struct {
		name   string
		actor  func(team) string
		target string
		want   int
	}{
		{"owner removes an admin", func(tm team) string { return tm.owner }, "admin@example.com", http.StatusNoContent},
		{"owner removes a member", func(tm team) string { return tm.owner }, "member@example.com", http.StatusNoContent},
		{"owner cannot remove themselves", func(tm team) string { return tm.owner }, "owner@example.com", http.StatusForbidden},
		{"admin removes a member", func(tm team) string { return tm.admin }, "member@example.com", http.StatusNoContent},
		{"admin cannot remove another admin", func(tm team) string { return tm.admin }, "admin@example.com", http.StatusForbidden},
		{"admin cannot remove the owner", func(tm team) string { return tm.admin }, "owner@example.com", http.StatusForbidden},
		{"member cannot remove anyone", func(tm team) string { return tm.member }, "admin@example.com", http.StatusForbidden},
		{"outsiders get 404", func(tm team) string { return tm.outside }, "member@example.com", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newStack(t)
			tm := s.newTeam()

			res := s.do(http.MethodDelete, tm.memberPath(s.userID(tt.target)), "", bearer(tt.actor(tm)))

			require.Equal(t, tt.want, res.Code, res.Body.String())
			_, stillMember := s.roleOf(tm.orgID, tt.target)
			assert.Equal(t, tt.want != http.StatusNoContent, stillMember)
		})
	}
}

func TestRemoveMember_TheRemovedUserLosesAccessAtOnce(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam()
	require.Equal(t, http.StatusOK, s.get(orgPath(tm.orgID), bearer(tm.member)).Code)

	require.Equal(t, http.StatusNoContent, s.do(http.MethodDelete, tm.memberPath(s.userID("member@example.com")), "", bearer(tm.admin)).Code)

	assert.Equal(t, http.StatusNotFound, s.get(orgPath(tm.orgID), bearer(tm.member)).Code)
	assert.Equal(t, http.StatusNotFound, s.get(tm.membersPath(), bearer(tm.member)).Code)
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodDelete, tm.memberPath(s.userID("member@example.com")), "", bearer(tm.admin)).Code,
		"removing someone twice is 404")
}

func TestLeave_MembersAndAdminsMayButTheOwnerMustTransferFirst(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam()
	leave := orgPath(tm.orgID) + "/leave"

	owner := s.post(leave, ``, bearer(tm.owner))
	assert.Equal(t, http.StatusForbidden, owner.Code)
	assert.Contains(t, owner.Body.String(), "owner_must_transfer_ownership")
	assert.Equal(t, http.StatusNotFound, s.post(leave, ``, bearer(tm.outside)).Code)

	assert.Equal(t, http.StatusNoContent, s.post(leave, ``, bearer(tm.member)).Code)
	assert.Equal(t, http.StatusNoContent, s.post(leave, ``, bearer(tm.admin)).Code)

	assert.Equal(t, http.StatusNotFound, s.get(orgPath(tm.orgID), bearer(tm.member)).Code)
	_, ownerStays := s.roleOf(tm.orgID, "owner@example.com")
	assert.True(t, ownerStays)
	for _, it := range s.get(orgsPath, bearer(tm.member)).json()["items"].([]any) {
		assert.NotEqual(t, tm.orgID, it.(map[string]any)["id"], "no longer listed")
	}
}

func (tm team) transferPath() string { return orgPath(tm.orgID) + "/transfer-ownership" }

func transferBody(userID, password string) string {
	return `{"user_id":"` + userID + `","current_password":"` + password + `"}`
}

// ownerRow returns the owner recorded on the organization and the user whose
// membership has the owner role: they must always agree.
func (s *stack) ownerRow(orgID string) (recorded, byRole string) {
	s.t.Helper()
	require.NoError(s.t, s.queryRow(`SELECT o.owner_id::text, (SELECT user_id::text FROM memberships WHERE organization_id = o.id AND role = 'owner')
		FROM organizations o WHERE o.id = $1`, orgID).Scan(&recorded, &byRole))
	return recorded, byRole
}

func TestTransferOwnership_SwapsRolesAndOwnerTogether(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam()
	admin := s.userID("admin@example.com")

	res := s.post(tm.transferPath(), transferBody(admin, "correct-horse"), bearer(tm.owner))

	require.Equal(t, http.StatusNoContent, res.Code, res.Body.String())
	recorded, byRole := s.ownerRow(tm.orgID)
	assert.Equal(t, admin, recorded)
	assert.Equal(t, admin, byRole)
	oldRole, _ := s.roleOf(tm.orgID, "owner@example.com")
	assert.Equal(t, "admin", oldRole, "the previous owner stays as an admin")

	// The new owner holds the owner powers and the old owner lost them, immediately.
	assert.Equal(t, http.StatusForbidden, s.do(http.MethodDelete, orgPath(tm.orgID), "", bearer(tm.owner)).Code)
	assert.Equal(t, http.StatusForbidden, s.post(tm.transferPath(), transferBody(admin, "correct-horse"), bearer(tm.owner)).Code)
	assert.Equal(t, http.StatusNoContent, s.post(orgPath(tm.orgID)+"/leave", ``, bearer(tm.owner)).Code, "no longer the owner, so leaving is allowed")
	assert.Equal(t, http.StatusNoContent, s.do(http.MethodDelete, orgPath(tm.orgID), "", bearer(tm.admin)).Code)
}

func TestTransferOwnership_NeedsTheCorrectPasswordAndChangesNothingOtherwise(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam()
	admin := s.userID("admin@example.com")

	res := s.post(tm.transferPath(), transferBody(admin, "wrong-password"), bearer(tm.owner))

	require.Equal(t, http.StatusBadRequest, res.Code)
	assert.Contains(t, res.Body.String(), "current_password")
	recorded, byRole := s.ownerRow(tm.orgID)
	assert.Equal(t, s.userID("owner@example.com"), recorded)
	assert.Equal(t, recorded, byRole)

	for range 4 {
		s.post(tm.transferPath(), transferBody(admin, "wrong-password"), bearer(tm.owner))
	}
	assert.Equal(t, http.StatusTooManyRequests, s.post(tm.transferPath(), transferBody(admin, "correct-horse"), bearer(tm.owner)).Code,
		"guessing the password is rate limited like every sensitive action")
}

func TestTransferOwnership_Rejections(t *testing.T) {
	tests := []struct {
		name  string
		actor func(team) string
		body  func(s *stack) string
		want  int
	}{
		{"an admin cannot transfer", func(tm team) string { return tm.admin }, func(s *stack) string { return transferBody(s.userID("member@example.com"), "correct-horse") }, http.StatusForbidden},
		{"a member cannot transfer", func(tm team) string { return tm.member }, func(s *stack) string { return transferBody(s.userID("admin@example.com"), "correct-horse") }, http.StatusForbidden},
		{"an outsider gets 404", func(tm team) string { return tm.outside }, func(s *stack) string { return transferBody(s.userID("admin@example.com"), "correct-horse") }, http.StatusNotFound},
		{"to yourself", func(tm team) string { return tm.owner }, func(s *stack) string { return transferBody(s.userID("owner@example.com"), "correct-horse") }, http.StatusBadRequest},
		{"to a non-member", func(tm team) string { return tm.owner }, func(s *stack) string { return transferBody(s.userID("outsider@example.com"), "correct-horse") }, http.StatusNotFound},
		{"to an unknown user", func(tm team) string { return tm.owner }, func(*stack) string { return transferBody(uuid.NewString(), "correct-horse") }, http.StatusNotFound},
		{"without a password", func(tm team) string { return tm.owner }, func(s *stack) string { return `{"user_id":"` + s.userID("admin@example.com") + `"}` }, http.StatusBadRequest},
		{"without a target", func(tm team) string { return tm.owner }, func(*stack) string { return `{"current_password":"correct-horse"}` }, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newStack(t)
			tm := s.newTeam()

			res := s.post(tm.transferPath(), tt.body(s), bearer(tt.actor(tm)))

			assert.Equal(t, tt.want, res.Code, res.Body.String())
			recorded, byRole := s.ownerRow(tm.orgID)
			assert.Equal(t, s.userID("owner@example.com"), recorded, "ownership did not move")
			assert.Equal(t, recorded, byRole)
		})
	}
}
