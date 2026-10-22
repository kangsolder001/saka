package entitlement

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/httpx"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

// Transactor runs a function in a transaction pinned to the tenant on ctx;
// *db.DB implements it.
type Transactor interface {
	WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error
}

var _ Transactor = (*db.DB)(nil)

// RequireFeature is a route middleware for resources only some plans include
// (spec §9.2): it answers 403 PLAN_FEATURE_UNAVAILABLE, with the current plan
// and the cheapest plan that has the feature, unless the organization's
// effective plan includes feature. It must run after the tenant middleware
// (org.TenantRoutes), which puts the organization on the context; it reads the
// plan in its own short transaction pinned to that tenant.
//
//	exports := org.TenantRoutes(v1, authenticate, members)
//	exports.GET("/exports/members.csv", entitlements.RequireFeature("export_csv"), h.exportMembers)
func (s *Service) RequireFeature(feature string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		orgID, ok := reqctx.OrgID(ctx)
		if !ok {
			// Wiring bug: the route was mounted without the tenant middleware.
			httpx.Error(c, apperr.Internal(errors.New("entitlement: RequireFeature on a route without a tenant")))
			return
		}
		err := s.tx.WithTx(ctx, func(tx pgx.Tx) error {
			return s.CheckFeature(ctx, tx, orgID, feature)
		})
		if err != nil {
			httpx.Error(c, err)
			return
		}
		c.Next()
	}
}
