package entitlement_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/entitlement"
	"github.com/kangsolder001/saka/internal/entitlement/mocks"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

func init() { gin.SetMode(gin.TestMode) }

// fakeTransactor runs fn without a database: the plan source is a mock, so the
// transaction handle is never used.
type fakeTransactor struct{ calls int }

func (f *fakeTransactor) WithTx(_ context.Context, fn func(tx pgx.Tx) error) error {
	f.calls++
	return fn(nil)
}

// serve mounts RequireFeature in front of a handler that records whether it ran,
// and sends one request, with the tenant on the context when withTenant is set.
func serve(t *testing.T, svc *entitlement.Service, feature string, withTenant bool) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	reached := false
	r := gin.New()
	r.GET("/x", func(c *gin.Context) {
		if withTenant {
			c.Request = c.Request.WithContext(reqctx.WithOrgID(c.Request.Context(), orgID))
		}
	}, svc.RequireFeature(feature), func(c *gin.Context) {
		reached = true
		c.Status(http.StatusNoContent)
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil))
	return rec, reached
}

func TestRequireFeature_LetsThroughAnOrganizationWhosePlanHasTheFeature(t *testing.T) {
	f := newFixture(t, pro, 0)
	tx := &fakeTransactor{}
	svc := entitlement.NewService(f.plans, tx)

	rec, reached := serve(t, svc, "export_csv", true)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.True(t, reached)
	assert.Equal(t, 1, tx.calls, "the check runs in its own transaction")
}

func TestRequireFeature_RefusesWithTheUpgradePathAndNeverReachesTheHandler(t *testing.T) {
	f := newFixture(t, pro, 0)
	svc := entitlement.NewService(f.plans, &fakeTransactor{})

	rec, reached := serve(t, svc, "audit_log", true)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.False(t, reached)
	var body struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "PLAN_FEATURE_UNAVAILABLE", body.Error.Code)
	assert.Equal(t, "Fitur ini tidak tersedia di paket Pro.", body.Error.Message)
	assert.Equal(t, map[string]any{"feature": "audit_log", "plan": "pro", "upgrade_to": "business"}, body.Error.Details)
}

func TestRequireFeature_AFreeOrganizationIsPointedAtTheCheapestPlanWithIt(t *testing.T) {
	f := newFixture(t, free, 0)
	svc := entitlement.NewService(f.plans, &fakeTransactor{})

	rec, _ := serve(t, svc, "export_csv", true)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), `"upgrade_to":"pro"`)
}

func TestRequireFeature_OnARouteWithoutATenantFailsLoudly(t *testing.T) {
	plans := mocks.NewMockPlanSource(t) // never consulted
	svc := entitlement.NewService(plans, &fakeTransactor{})

	rec, reached := serve(t, svc, "export_csv", false)

	assert.Equal(t, http.StatusInternalServerError, rec.Code, "a wiring mistake must not open the feature")
	assert.False(t, reached)
}

func TestRequireFeature_APlanLookupFailureDeniesAccess(t *testing.T) {
	plans := mocks.NewMockPlanSource(t)
	plans.EXPECT().EffectivePlan(mock.Anything, mock.Anything, orgID).Return(entitlement.Plan{}, errors.New("db down"))
	svc := entitlement.NewService(plans, &fakeTransactor{})

	rec, reached := serve(t, svc, "export_csv", true)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.False(t, reached)
	assert.NotContains(t, rec.Body.String(), "db down", "the cause stays in the logs")
}
