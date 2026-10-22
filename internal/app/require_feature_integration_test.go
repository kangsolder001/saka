//go:build integration

package app_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/app"
	"github.com/kangsolder001/saka/internal/entitlement"
)

// featureRoutes mounts the kind of routes later modules will have: resources
// that only some plans include. They exist only in these tests; no production
// route is gated yet.
func featureRoutes(o *app.Options) {
	o.TenantRoutes = func(rg *gin.RouterGroup, entitlements *entitlement.Service) {
		rg.GET("/exports/members.csv", entitlements.RequireFeature("export_csv"), func(c *gin.Context) {
			c.String(http.StatusOK, "id,email\n")
		})
		rg.GET("/audit-log", entitlements.RequireFeature("audit_log"), func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"items": []any{}})
		})
	}
}

func (tm team) exportPath() string   { return orgPath(tm.orgID) + "/exports/members.csv" }
func (tm team) auditLogPath() string { return orgPath(tm.orgID) + "/audit-log" }

func TestRequireFeature_FreeOrganizationsAreRefusedWithAnUpgradePath(t *testing.T) {
	s := newStack(t, featureRoutes)
	tm := s.soloTeam()

	res := s.get(tm.exportPath(), bearer(tm.owner))

	require.Equal(t, http.StatusForbidden, res.Code, res.Body.String())
	assert.Equal(t, "PLAN_FEATURE_UNAVAILABLE", errorOf(res)["code"])
	assert.Equal(t, "Fitur ini tidak tersedia di paket Free.", errorOf(res)["message"])
	assert.Equal(t, map[string]any{"feature": "export_csv", "plan": "free", "upgrade_to": "pro"}, detailsOf(res))
	assert.Equal(t, "business", detailsOf(s.get(tm.auditLogPath(), bearer(tm.owner)))["upgrade_to"])
}

func TestRequireFeature_EachPlanGetsExactlyItsOwnFeatures(t *testing.T) {
	tests := []struct {
		plan                string
		export, auditStatus int
	}{
		{"free", http.StatusForbidden, http.StatusForbidden},
		{"pro", http.StatusOK, http.StatusForbidden},
		{"business", http.StatusOK, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.plan, func(t *testing.T) {
			s := newStack(t, featureRoutes)
			tm := s.soloTeam()
			s.setPlan(tm.orgID, tt.plan, "active")

			assert.Equal(t, tt.export, s.get(tm.exportPath(), bearer(tm.owner)).Code, "export_csv")
			assert.Equal(t, tt.auditStatus, s.get(tm.auditLogPath(), bearer(tm.owner)).Code, "audit_log")
		})
	}
}

func TestRequireFeature_ThePlanIsReadOnEveryRequestSoChangesApplyAtOnce(t *testing.T) {
	s := newStack(t, featureRoutes)
	tm := s.soloTeam()
	require.Equal(t, http.StatusForbidden, s.get(tm.exportPath(), bearer(tm.owner)).Code)

	s.setPlan(tm.orgID, "pro", "active")
	assert.Equal(t, http.StatusOK, s.get(tm.exportPath(), bearer(tm.owner)).Code, "upgrade: no re-login, no cache to wait out")

	s.setPlan(tm.orgID, "pro", "expired")
	assert.Equal(t, http.StatusForbidden, s.get(tm.exportPath(), bearer(tm.owner)).Code, "an expired organization is back on Free")
}

func TestRequireFeature_TrialingAndPastDueWithinGraceKeepTheFeature(t *testing.T) {
	s := newStack(t, featureRoutes)
	tm := s.soloTeam()

	s.setPlan(tm.orgID, "pro", "trialing")
	assert.Equal(t, http.StatusOK, s.get(tm.exportPath(), bearer(tm.owner)).Code)

	s.setPlan(tm.orgID, "pro", "past_due")
	assert.Equal(t, http.StatusForbidden, s.get(tm.exportPath(), bearer(tm.owner)).Code, "past_due without a grace period grants nothing")
	_, err := s.admin.Exec(s.ctx(), "UPDATE subscriptions SET grace_until = $2 WHERE organization_id = $1", tm.orgID, s.clock.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, s.get(tm.exportPath(), bearer(tm.owner)).Code)
}

func TestRequireFeature_TheTenantMiddlewareRunsFirst(t *testing.T) {
	s := newStack(t, featureRoutes)
	tm := s.soloTeam()
	s.setPlan(tm.orgID, "business", "active")
	s.registered("outsider@example.com")
	outsider := s.loggedIn("outsider@example.com")

	assert.Equal(t, http.StatusNotFound, s.get(tm.exportPath(), bearer(outsider)).Code, "a non-member learns nothing about the plan either")
	assert.Equal(t, http.StatusUnauthorized, s.get(tm.exportPath(), nil).Code)
}
