package org_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/org"
	orgmocks "github.com/kangsolder001/saka/internal/org/mocks"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

func init() { gin.SetMode(gin.TestMode) }

// seen is what the handler behind the tenant middleware observed.
type seen struct {
	called  bool
	orgID   uuid.UUID
	hasOrg  bool
	role    org.Role
	hasRole bool
}

// tenantRouter mounts Tenant behind a stand-in for the authentication
// middleware (which would set the user ID) and records what reaches the handler.
func tenantRouter(members org.MembershipReader, userID *uuid.UUID) (*gin.Engine, *seen) {
	got := &seen{}
	r := gin.New()
	g := r.Group("/orgs/:orgId", func(c *gin.Context) {
		if userID != nil {
			c.Request = c.Request.WithContext(reqctx.WithUserID(c.Request.Context(), *userID))
		}
	}, org.Tenant(members))
	g.GET("/ping", func(c *gin.Context) {
		got.called = true
		ctx := c.Request.Context()
		got.orgID, got.hasOrg = reqctx.OrgID(ctx)
		got.role, got.hasRole = org.RoleFrom(ctx)
		c.Status(http.StatusNoContent)
	})
	return r, got
}

func get(r http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestTenant_MemberGetsOrganizationAndRoleOnTheContext(t *testing.T) {
	members := orgmocks.NewMockMembershipReader(t)
	orgID, userID := uuid.New(), uuid.New()
	members.EXPECT().Membership(mock.Anything, orgID, userID).
		Return(org.Membership{OrganizationID: orgID, UserID: userID, Role: org.RoleAdmin}, nil).Once()
	r, got := tenantRouter(members, &userID)

	rec := get(r, "/orgs/"+orgID.String()+"/ping")

	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.True(t, got.called)
	assert.True(t, got.hasOrg)
	assert.Equal(t, orgID, got.orgID)
	assert.True(t, got.hasRole)
	assert.Equal(t, org.RoleAdmin, got.role)
}

func TestTenant_NonMemberGets404AndTheHandlerNeverRuns(t *testing.T) {
	members := orgmocks.NewMockMembershipReader(t)
	orgID, userID := uuid.New(), uuid.New()
	members.EXPECT().Membership(mock.Anything, orgID, userID).Return(org.Membership{}, apperr.NotFound()).Once()
	r, got := tenantRouter(members, &userID)

	rec := get(r, "/orgs/"+orgID.String()+"/ping")

	assert.Equal(t, http.StatusNotFound, rec.Code, "404, not 403: the organization's existence must not leak")
	assert.Contains(t, rec.Body.String(), `"code":"NOT_FOUND"`)
	assert.False(t, got.called)
}

func TestTenant_MalformedOrganizationIDIs404WithoutALookup(t *testing.T) {
	members := orgmocks.NewMockMembershipReader(t) // no expectations: any lookup fails the test
	userID := uuid.New()
	r, got := tenantRouter(members, &userID)

	for _, id := range []string{"not-a-uuid", "123", "00000000-0000-0000-0000-00000000000g"} {
		rec := get(r, "/orgs/"+id+"/ping")
		assert.Equal(t, http.StatusNotFound, rec.Code, id)
	}
	assert.False(t, got.called)
}

func TestTenant_WithoutAnAuthenticatedUserIs401(t *testing.T) {
	members := orgmocks.NewMockMembershipReader(t)
	r, got := tenantRouter(members, nil)

	rec := get(r, "/orgs/"+uuid.NewString()+"/ping")

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.False(t, got.called)
}

func TestTenant_LookupFailureIs500NotAnAccessDecision(t *testing.T) {
	members := orgmocks.NewMockMembershipReader(t)
	orgID, userID := uuid.New(), uuid.New()
	members.EXPECT().Membership(mock.Anything, orgID, userID).Return(org.Membership{}, errors.New("database down")).Once()
	r, got := tenantRouter(members, &userID)

	rec := get(r, "/orgs/"+orgID.String()+"/ping")

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.False(t, got.called)
	assert.NotContains(t, rec.Body.String(), "database down")
}

func TestRoleFrom_IsAbsentOutsideTenantRoutes(t *testing.T) {
	_, ok := org.RoleFrom(context.Background())

	assert.False(t, ok)
}
