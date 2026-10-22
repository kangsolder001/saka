//go:build integration

package app_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const plansPath = "/api/v1/plans"

func (tm team) subscriptionPath() string { return orgPath(tm.orgID) + "/subscription" }

func subscriptionOf(res response) map[string]any {
	return res.json()["subscription"].(map[string]any)
}

func TestListPlans_IsPublicAndShowsTheActiveCatalogueInOrder(t *testing.T) {
	s := newStack(t)

	res := s.get(plansPath, nil)

	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	items := res.json()["items"].([]any)
	require.Len(t, items, 3)
	codes := make([]any, len(items))
	for i, it := range items {
		codes[i] = it.(map[string]any)["code"]
	}
	assert.Equal(t, []any{"free", "pro", "business"}, codes)
	assert.Equal(t, map[string]any{
		"code": "pro", "name": "Pro", "price_amount": 99000.0, "currency": "IDR", "interval": "month", "trial_days": 14.0,
		"features": []any{"export_csv"}, "limits": map[string]any{"members": 10.0},
	}, items[1], "everything the plans page needs, and no internal IDs")
	assert.Equal(t, map[string]any{"members": -1.0}, items[2].(map[string]any)["limits"], "-1 is unlimited")
	assert.Equal(t, []any{}, items[0].(map[string]any)["features"], "no features is an empty list, not null")
}

func TestListPlans_HidesDeactivatedPlans(t *testing.T) {
	s := newStack(t)
	_, err := s.admin.Exec(s.ctx(), "UPDATE plans SET is_active = false WHERE code = 'business'")
	require.NoError(t, err)

	items := s.get(plansPath, nil).json()["items"].([]any)

	require.Len(t, items, 2)
	assert.Equal(t, "pro", items[1].(map[string]any)["code"])
}

func TestGetSubscription_ANewOrganizationIsActiveOnFreeWithItsUsage(t *testing.T) {
	s := newStack(t)
	tm := s.soloTeam()

	res := s.get(tm.subscriptionPath(), bearer(tm.owner))

	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	sub := subscriptionOf(res)
	assert.Equal(t, map[string]any{
		"plan": "free", "status": "active", "trial_ends_at": nil,
		"current_period_start": epoch.Format(time.RFC3339), "current_period_end": nil,
		"cancel_at_period_end": false, "pending_plan": nil, "grace_until": nil,
		"effective_plan": "free",
		"usage":          map[string]any{"members": map[string]any{"used": 1.0, "max": 3.0}},
	}, sub)
}

func TestGetSubscription_UsageCountsMembersAndPendingInvitationsAgainstThePlan(t *testing.T) {
	s := newStack(t)
	tm := s.soloTeam()
	s.fillFree(tm, "a@example.com", "b@example.com")
	members := func() map[string]any {
		return subscriptionOf(s.get(tm.subscriptionPath(), bearer(tm.owner)))["usage"].(map[string]any)["members"].(map[string]any)
	}

	assert.Equal(t, map[string]any{"used": 3.0, "max": 3.0}, members())

	s.setPlan(tm.orgID, "pro", "active")
	assert.Equal(t, map[string]any{"used": 3.0, "max": 10.0}, members())

	s.setPlan(tm.orgID, "business", "active")
	assert.Equal(t, map[string]any{"used": 3.0, "max": -1.0}, members(), "-1 is unlimited")

	require.Equal(t, http.StatusNoContent, s.do(http.MethodDelete, tm.invitationPath(s.openInvitationID(tm.orgID, "a@example.com")), "", bearer(tm.owner)).Code)
	assert.Equal(t, map[string]any{"used": 2.0, "max": -1.0}, members(), "usage is counted from the rows, so it follows them")
}

func TestGetSubscription_AnExpiredSubscriptionShowsItsOwnPlanButTheEffectiveFreeOne(t *testing.T) {
	s := newStack(t)
	tm := s.soloTeam()
	s.setPlan(tm.orgID, "pro", "expired")

	sub := subscriptionOf(s.get(tm.subscriptionPath(), bearer(tm.owner)))

	assert.Equal(t, "pro", sub["plan"], "what the organization subscribed to")
	assert.Equal(t, "expired", sub["status"])
	assert.Equal(t, "free", sub["effective_plan"], "what it may use right now")
	assert.Equal(t, map[string]any{"used": 1.0, "max": 3.0}, sub["usage"].(map[string]any)["members"], "against Free's limit")
}

func TestGetSubscription_ReportsTrialGraceAndScheduledChanges(t *testing.T) {
	s := newStack(t)
	tm := s.soloTeam()
	s.setPlan(tm.orgID, "business", "trialing")
	_, err := s.admin.Exec(s.ctx(), `
		UPDATE subscriptions SET cancel_at_period_end = true, grace_until = $2,
		       current_period_end = $2, pending_plan_id = (SELECT id FROM plans WHERE code = 'pro')
		WHERE organization_id = $1`, tm.orgID, epoch.Add(24*time.Hour))
	require.NoError(t, err)

	sub := subscriptionOf(s.get(tm.subscriptionPath(), bearer(tm.owner)))

	assert.Equal(t, "business", sub["plan"])
	assert.Equal(t, "trialing", sub["status"])
	assert.NotNil(t, sub["trial_ends_at"])
	assert.Equal(t, true, sub["cancel_at_period_end"])
	assert.Equal(t, "pro", sub["pending_plan"], "a scheduled downgrade is named by plan code")
	assert.Equal(t, epoch.Add(24*time.Hour).Format(time.RFC3339), sub["grace_until"])
	assert.Equal(t, epoch.Add(24*time.Hour).Format(time.RFC3339), sub["current_period_end"])
	assert.Equal(t, "business", sub["effective_plan"])
}

func TestGetSubscription_AnyMemberMayReadItButOutsidersMayNot(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam()

	for role, token := range map[string]string{"owner": tm.owner, "admin": tm.admin, "member": tm.member} {
		res := s.get(tm.subscriptionPath(), bearer(token))
		assert.Equal(t, http.StatusOK, res.Code, role)
		assert.Equal(t, "free", subscriptionOf(res)["plan"], role)
	}
	assert.Equal(t, http.StatusNotFound, s.get(tm.subscriptionPath(), bearer(tm.outside)).Code, "someone else's organization")
	assert.Equal(t, http.StatusUnauthorized, s.get(tm.subscriptionPath(), nil).Code)
}

func TestGetSubscription_NeverShowsAnotherOrganizationsSubscription(t *testing.T) {
	s := newStack(t)
	a := s.soloTeam()
	s.registered("bella@example.com")
	bOrg := s.firstOrg("bella@example.com")
	s.setPlan(bOrg, "business", "active")

	sub := subscriptionOf(s.get(a.subscriptionPath(), bearer(a.owner)))

	assert.Equal(t, "free", sub["plan"])
	assert.Equal(t, http.StatusNotFound, s.get(orgPath(bOrg)+"/subscription", bearer(a.owner)).Code)
}
