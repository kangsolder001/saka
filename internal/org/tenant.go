package org

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/httpx"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

// OrgIDParam is the route parameter naming the organization: /orgs/:orgId/...
const OrgIDParam = "orgId"

type roleKey struct{}

// WithRole returns a context carrying the caller's role in the current
// organization. Only the tenant middleware should call it, never user input.
func WithRole(ctx context.Context, role Role) context.Context {
	return context.WithValue(ctx, roleKey{}, role)
}

// RoleFrom returns the caller's role in the current organization and whether
// the context has one (it does inside /orgs/{orgId}/... routes).
func RoleFrom(ctx context.Context) (Role, bool) {
	role, ok := ctx.Value(roleKey{}).(Role)
	return role, ok
}

// MembershipReader is what the tenant middleware needs; *Service implements it.
type MembershipReader interface {
	Membership(ctx context.Context, orgID, userID uuid.UUID) (Membership, error)
}

var _ MembershipReader = (*Service)(nil)

// Tenant is the middleware for every /orgs/:orgId/... route (spec §7.3). It
// must run after the authentication middleware. It looks up the caller's
// membership on every request (roles are never cached in the token or
// elsewhere, so a role change takes effect immediately) and stores the
// organization ID and the role on the request context. Callers who are not
// members get 404, not 403, so the existence of an organization does not leak.
//
// From here on, handlers and services read the tenant only from the context
// (reqctx.OrgID, RoleFrom), never from the URL or the body.
func Tenant(members MembershipReader) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := reqctx.UserID(c.Request.Context())
		if !ok {
			// Wiring bug: the route was mounted without the authentication middleware.
			httpx.Error(c, apperr.Unauthenticated())
			return
		}
		orgID, err := uuid.Parse(c.Param(OrgIDParam))
		if err != nil {
			httpx.Error(c, apperr.NotFound())
			return
		}
		m, err := members.Membership(c.Request.Context(), orgID, userID)
		if err != nil {
			httpx.Error(c, err)
			return
		}
		ctx := reqctx.WithOrgID(c.Request.Context(), m.OrganizationID)
		ctx = WithRole(ctx, m.Role)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
