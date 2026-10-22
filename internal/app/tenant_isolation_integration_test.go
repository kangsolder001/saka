//go:build integration

package app_test

import (
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const tenantRoutePrefix = "/api/v1/orgs/:orgId"

// tenantRoutes lists every registered route that lives under /orgs/:orgId, as
// "METHOD path" with the path pattern.
func (s *stack) tenantRoutes() []string {
	var out []string
	for _, r := range s.engine.Routes() {
		if r.Path == tenantRoutePrefix || strings.HasPrefix(r.Path, tenantRoutePrefix+"/") {
			out = append(out, r.Method+" "+r.Path)
		}
	}
	sort.Strings(out)
	return out
}

// fill turns a route pattern into a concrete URL for the given organization;
// every other parameter becomes a fresh UUID.
func fill(path, orgID string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		switch {
		case p == ":orgId":
			parts[i] = orgID
		case strings.HasPrefix(p, ":"):
			parts[i] = uuid.NewString()
		}
	}
	return strings.Join(parts, "/")
}

// Every route that belongs to an organization must answer "404" to someone who
// is not a member, whatever the method and whatever the body, and 401 without a
// login. The list comes from the router itself, so a route added later is
// covered automatically and cannot ship without the tenant middleware in front
// of it.
func TestTenantIsolation_EveryOrganizationRouteHidesItFromNonMembers(t *testing.T) {
	s := newStack(t)
	tm := s.verifiedTeam()
	s.verify("outsider@example.com")
	outsider := s.loggedIn("outsider@example.com") // verified, so only membership can stop them

	routes := s.tenantRoutes()
	require.NotEmpty(t, routes)
	for _, route := range routes {
		method, path, _ := strings.Cut(route, " ")
		url := fill(path, tm.orgID)
		body := `{}`
		if method == http.MethodGet || method == http.MethodDelete {
			body = ""
		}

		asOutsider := s.do(method, url, body, bearer(outsider))
		anonymous := s.do(method, url, body, nil)

		assert.Equal(t, http.StatusNotFound, asOutsider.Code, "%s as a non-member: %s", route, asOutsider.Body.String())
		assert.Contains(t, asOutsider.Body.String(), `"code":"NOT_FOUND"`, route)
		assert.Equal(t, http.StatusUnauthorized, anonymous.Code, "%s without a login", route)
	}
}

// The router inventory above is only as good as its coverage of what exists:
// pin the expected set, so adding a tenant route is a conscious change that
// also adds its role tests.
func TestTenantIsolation_TheOrganizationRouteInventory(t *testing.T) {
	s := newStack(t)

	assert.Equal(t, []string{
		"DELETE /api/v1/orgs/:orgId",
		"DELETE /api/v1/orgs/:orgId/invitations/:id",
		"DELETE /api/v1/orgs/:orgId/members/:userId",
		"GET /api/v1/orgs/:orgId",
		"GET /api/v1/orgs/:orgId/invitations",
		"GET /api/v1/orgs/:orgId/members",
		"PATCH /api/v1/orgs/:orgId",
		"PATCH /api/v1/orgs/:orgId/members/:userId",
		"POST /api/v1/orgs/:orgId/invitations",
		"POST /api/v1/orgs/:orgId/invitations/:id/resend",
		"POST /api/v1/orgs/:orgId/leave",
		"POST /api/v1/orgs/:orgId/transfer-ownership",
	}, s.tenantRoutes(), "update this list (and the role tests) when a route under /orgs/{orgId} is added")
}

func TestTenantIsolation_AUserOfOrganizationACannotReachOrganizationB(t *testing.T) {
	s := newStack(t)
	a := s.verifiedTeam()
	s.registered("bella@example.com")
	s.verify("bella@example.com")
	bellaToken := s.loggedIn("bella@example.com")
	bOrg := s.firstOrg("bella@example.com")
	b := team{orgID: bOrg, owner: bellaToken}
	bInvitation := s.post(b.invitationsPath(), inviteBody("guest@example.com", "member"), bearer(bellaToken)).json()["invitation"].(map[string]any)["id"].(string)

	// A's owner holds every power in A and none in B, by URL or by smuggling B's identifiers into A's routes.
	assert.Equal(t, http.StatusNotFound, s.get(orgPath(bOrg), bearer(a.owner)).Code)
	assert.Equal(t, http.StatusNotFound, s.patch(orgPath(bOrg), `{"name":"hijacked"}`, bearer(a.owner)).Code)
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodDelete, orgPath(bOrg), "", bearer(a.owner)).Code)
	assert.Equal(t, http.StatusNotFound, s.get(b.membersPath(), bearer(a.owner)).Code)
	assert.Equal(t, http.StatusNotFound, s.get(b.invitationsPath(), bearer(a.owner)).Code)
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodDelete, a.invitationPath(bInvitation), "", bearer(a.owner)).Code, "B's invitation inside A's URL")
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodDelete, a.memberPath(s.userID("bella@example.com")), "", bearer(a.owner)).Code, "B's owner inside A's URL")
	assert.Equal(t, http.StatusNotFound, s.patch(a.memberPath(s.userID("bella@example.com")), roleBody("admin"), bearer(a.owner)).Code)
	assert.Equal(t, http.StatusNotFound,
		s.post(a.transferPath(), transferBody(s.userID("bella@example.com"), "correct-horse"), bearer(a.owner)).Code, "ownership cannot be handed to a stranger")

	// Nothing of B changed, and B's data never showed up in A's listings.
	var name string
	require.NoError(t, s.queryRow("SELECT name FROM organizations WHERE id = $1", bOrg).Scan(&name))
	assert.Equal(t, "Acme", name)
	assert.Equal(t, 1, s.openInvitations(bOrg))
	for _, m := range s.get(a.membersPath(), bearer(a.owner)).json()["items"].([]any) {
		assert.NotEqual(t, "bella@example.com", m.(map[string]any)["email"])
	}
	for _, o := range s.get(orgsPath, bearer(a.owner)).json()["items"].([]any) {
		assert.NotEqual(t, bOrg, o.(map[string]any)["id"])
	}
}

func TestTenantIsolation_ATenantIdInTheBodyIsNeverTrusted(t *testing.T) {
	s := newStack(t)
	a := s.verifiedTeam()
	s.registered("bella@example.com")
	bOrg := s.firstOrg("bella@example.com")

	// Fields that would redirect an operation to another tenant are unknown fields.
	for _, body := range []string{
		`{"name":"X","organization_id":"` + bOrg + `"}`,
		`{"name":"X","org_id":"` + bOrg + `"}`,
	} {
		assert.Equal(t, http.StatusBadRequest, s.post(orgsPath, body, bearer(a.owner)).Code)
		assert.Equal(t, http.StatusBadRequest, s.patch(orgPath(a.orgID), body, bearer(a.owner)).Code)
	}
	assert.Equal(t, http.StatusBadRequest, s.post(a.invitationsPath(),
		`{"email":"x@example.com","role":"member","organization_id":"`+bOrg+`"}`, bearer(a.owner)).Code)
}
