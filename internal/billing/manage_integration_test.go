//go:build integration

package billing_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/billing"
	"github.com/kangsolder001/saka/internal/entitlement"
	"github.com/kangsolder001/saka/internal/org"
	"github.com/kangsolder001/saka/internal/platform/apperr"
)

// activeOn puts the organization on an active paid plan whose period ends in 10 days.
func (e *payEnv) activeOn(orgID uuid.UUID, plan string) {
	e.t.Helper()
	end := epoch.Add(10 * day)
	e.setSub(orgID, subSpec{Plan: plan, Status: billing.StatusActive, PeriodStart: end.AddDate(0, -1, 0), PeriodEnd: &end})
}

func TestChangePlan_AnUpgradeIsAnInvoiceAndNothingChangesUntilItIsPaid(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activeOn(orgID, "pro")
	before := e.sub(orgID)

	res, err := e.svc.ChangePlan(ownerCtx(orgID), "business")

	require.NoError(t, err)
	require.NotNil(t, res.Checkout)
	assert.False(t, res.Scheduled)
	assert.Equal(t, "business", res.Checkout.Invoice.PlanCode)
	assert.Equal(t, int64(299000), res.Checkout.Invoice.Amount)
	assert.Equal(t, before, e.sub(orgID))
	assert.Len(t, e.emails("invoice_created"), 1)
}

func TestChangePlan_AFreeOrganizationUpgradesLikeACheckoutWithItsTrial(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()

	res, err := e.svc.ChangePlan(ownerCtx(orgID), "pro")

	require.NoError(t, err)
	require.NotNil(t, res.Checkout)
	assert.True(t, res.Checkout.TrialStarted)
	assert.Equal(t, billing.StatusTrialing, e.sub(orgID).Status)
}

func TestChangePlan_ADowngradeIsBookedForThePeriodEndAndWarnsAboutLimits(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activeOn(orgID, "business")
	e.insertRenewal(orgID, "INV-2026-000050", "business", epoch.Add(10*day))
	e.usage.byLimit = map[string]entitlement.LimitUsage{"members": {Used: 12, Max: -1}, "projects": {Used: 3, Max: -1}}

	res, err := e.svc.ChangePlan(ownerCtx(orgID), "pro")

	require.NoError(t, err)
	assert.True(t, res.Scheduled)
	assert.Nil(t, res.Checkout, "nothing to pay now")
	assert.Equal(t, []billing.LimitWarning{
		{Limit: "members", Used: 12, Max: 10},
		{Limit: "projects", Used: 3, Max: 0}, // a limit Pro does not list is zero (fail closed)
	}, res.Warnings, "data stays; only creating more will be blocked")

	sub := e.sub(orgID)
	assert.Equal(t, "business", sub.Plan, "the current plan stays until the period ends")
	require.NotNil(t, sub.PendingPlan)
	assert.Equal(t, "pro", *sub.PendingPlan)
	assert.Equal(t, billing.StatusActive, sub.Status)
	assert.Equal(t, "void", e.invoiceByNumber("INV-2026-000050").Status, "the renewal for the old plan is replaced")
	assert.Empty(t, e.gateway.Created())
}

func TestChangePlan_ADowngradeWithinTheLimitsHasNoWarnings(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activeOn(orgID, "business")
	e.usage.byLimit = map[string]entitlement.LimitUsage{"members": {Used: 4, Max: -1}}

	res, err := e.svc.ChangePlan(ownerCtx(orgID), "pro")

	require.NoError(t, err)
	assert.Empty(t, res.Warnings)
}

func TestChangePlan_AskingForTheCurrentPlanTakesBackAScheduledDowngrade(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activeOn(orgID, "business")
	_, err := e.svc.ChangePlan(ownerCtx(orgID), "pro")
	require.NoError(t, err)

	res, err := e.svc.ChangePlan(ownerCtx(orgID), "business")

	require.NoError(t, err)
	assert.True(t, res.Cleared)
	assert.Nil(t, e.sub(orgID).PendingPlan)

	_, err = e.svc.ChangePlan(ownerCtx(orgID), "business")
	require.Error(t, err)
	assert.True(t, apperr.IsCode(err, apperr.CodeConflict), "no downgrade left to take back, and already on this plan")
}

func TestChangePlan_BookingTheSameDowngradeTwiceChangesNothing(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activeOn(orgID, "business")
	_, err := e.svc.ChangePlan(ownerCtx(orgID), "pro")
	require.NoError(t, err)
	first := e.sub(orgID)

	res, err := e.svc.ChangePlan(ownerCtx(orgID), "pro")

	require.NoError(t, err)
	assert.True(t, res.Scheduled)
	assert.Equal(t, first.PendingPlan, e.sub(orgID).PendingPlan)
}

func TestChangePlan_ATrialOrPastDueSubscriptionSwitchesByBuyingTheNewPlan(t *testing.T) {
	t.Run("trialing", func(t *testing.T) {
		e := newPayEnv(t)
		orgID := e.newFreeOrg()
		e.buy(orgID, "business") // trial on Business

		res, err := e.svc.ChangePlan(ownerCtx(orgID), "pro") // cheaper, but a trial has no period to wait for

		require.NoError(t, err)
		require.NotNil(t, res.Checkout)
		assert.False(t, res.Scheduled)
		assert.Equal(t, "pro", res.Checkout.Invoice.PlanCode)
		assert.Equal(t, "business", e.sub(orgID).Plan)
	})
	t.Run("past due", func(t *testing.T) {
		e := newPayEnv(t)
		orgID := e.newFreeOrg()
		end := epoch.Add(-day)
		until := end.Add(3 * day)
		e.setSub(orgID, subSpec{Plan: "business", Status: billing.StatusPastDue, PeriodStart: end.AddDate(0, -1, 0), PeriodEnd: &end, GraceUntil: &until})

		res, err := e.svc.ChangePlan(ownerCtx(orgID), "pro")

		require.NoError(t, err)
		require.NotNil(t, res.Checkout)
		assert.Equal(t, billing.StatusPastDue, e.sub(orgID).Status)
	})
}

func TestChangePlan_AnEndedSubscriptionMustUseCheckout(t *testing.T) {
	for _, status := range []billing.Status{billing.StatusExpired, billing.StatusCanceled} {
		t.Run(string(status), func(t *testing.T) {
			e := newPayEnv(t)
			orgID := e.newFreeOrg()
			end := epoch.AddDate(0, 0, -5)
			e.setSub(orgID, subSpec{Plan: "pro", Status: status, TrialEndsAt: at(epoch.AddDate(0, -2, 0)), PeriodStart: end.AddDate(0, -1, 0), PeriodEnd: &end})

			_, err := e.svc.ChangePlan(ownerCtx(orgID), "business")

			require.Error(t, err)
			assert.True(t, apperr.IsCode(err, apperr.CodeConflict))
			assert.Equal(t, "use_checkout", apperr.From(err).Details["reason"])
			assert.Empty(t, e.invoices(orgID))
		})
	}
}

func TestChangePlan_RejectsBadPlansAndNonOwners(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activeOn(orgID, "business")

	for _, plan := range []string{"", "free", "platinum"} {
		_, err := e.svc.ChangePlan(ownerCtx(orgID), plan)
		assert.True(t, apperr.IsCode(err, apperr.CodeValidation), "%q: %v", plan, err)
	}
	for _, role := range []org.Role{org.RoleAdmin, org.RoleMember} {
		_, err := e.svc.ChangePlan(ctxAs(orgID, role), "pro")
		assert.True(t, apperr.IsCode(err, apperr.CodeForbidden), "%s: %v", role, err)
	}
	assert.Nil(t, e.sub(orgID).PendingPlan)
}

func TestCancelSubscription_EndsItAtThePeriodEndAndVoidsTheWaitingRenewal(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activeOn(orgID, "pro")
	e.insertRenewal(orgID, "INV-2026-000050", "pro", epoch.Add(10*day))
	before := e.sub(orgID)

	require.NoError(t, e.svc.CancelSubscription(ownerCtx(orgID)))

	sub := e.sub(orgID)
	assert.True(t, sub.CancelAtPeriodEnd)
	assert.Equal(t, billing.StatusActive, sub.Status, "access continues until the period ends")
	assert.Equal(t, before.CurrentPeriodEnd, sub.CurrentPeriodEnd)
	assert.Equal(t, "void", e.invoiceByNumber("INV-2026-000050").Status, "no payment should extend a subscription that is ending")

	require.NoError(t, e.svc.CancelSubscription(ownerCtx(orgID)), "idempotent")
	assert.True(t, e.sub(orgID).CancelAtPeriodEnd)
}

func TestResumeSubscription_TakesTheCancellationBack(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activeOn(orgID, "pro")
	require.NoError(t, e.svc.CancelSubscription(ownerCtx(orgID)))

	require.NoError(t, e.svc.ResumeSubscription(ownerCtx(orgID)))
	assert.False(t, e.sub(orgID).CancelAtPeriodEnd)

	require.NoError(t, e.svc.ResumeSubscription(ownerCtx(orgID)), "idempotent")
	assert.False(t, e.sub(orgID).CancelAtPeriodEnd)
}

func TestCancelAndResume_HaveNothingToDoForFreeExpiredOrCanceledSubscriptions(t *testing.T) {
	setups := map[string]func(e *payEnv, orgID uuid.UUID){
		"free": func(*payEnv, uuid.UUID) {},
		"expired": func(e *payEnv, orgID uuid.UUID) {
			end := epoch.AddDate(0, 0, -5)
			e.setSub(orgID, subSpec{Plan: "pro", Status: billing.StatusExpired, PeriodStart: end.AddDate(0, -1, 0), PeriodEnd: &end})
		},
		"canceled": func(e *payEnv, orgID uuid.UUID) {
			end := epoch.AddDate(0, 0, -5)
			e.setSub(orgID, subSpec{Plan: "pro", Status: billing.StatusCanceled, PeriodStart: end.AddDate(0, -1, 0), PeriodEnd: &end})
		},
	}
	for name, setup := range setups {
		t.Run(name, func(t *testing.T) {
			e := newPayEnv(t)
			orgID := e.newFreeOrg()
			setup(e, orgID)

			err := e.svc.CancelSubscription(ownerCtx(orgID))
			require.Error(t, err)
			assert.True(t, apperr.IsCode(err, apperr.CodeConflict), err.Error())
			assert.Equal(t, "nothing_to_cancel", apperr.From(err).Details["reason"])

			err = e.svc.ResumeSubscription(ownerCtx(orgID))
			require.Error(t, err)
			assert.Equal(t, "nothing_to_resume", apperr.From(err).Details["reason"])
		})
	}
}

func TestCancelAndResume_OnlyTheOwnerMay(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activeOn(orgID, "pro")

	for _, role := range []org.Role{org.RoleAdmin, org.RoleMember} {
		assert.True(t, apperr.IsCode(e.svc.CancelSubscription(ctxAs(orgID, role)), apperr.CodeForbidden), string(role))
		assert.True(t, apperr.IsCode(e.svc.ResumeSubscription(ctxAs(orgID, role)), apperr.CodeForbidden), string(role))
	}
	assert.True(t, apperr.IsCode(e.svc.CancelSubscription(context.Background()), apperr.CodeNotFound), "no tenant on the context")
	assert.False(t, e.sub(orgID).CancelAtPeriodEnd)
}
