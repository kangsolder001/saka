//go:build integration

package billing_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/billing"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

func TestStartFree_CreatesTheFreeSubscriptionAndRefusesASecondOne(t *testing.T) {
	e := newEnv(t)
	orgID := e.newOrg()

	e.inTenant(orgID, func(ctx context.Context, tx pgx.Tx) {
		require.NoError(t, e.svc.StartFree(ctx, tx, orgID))

		sub, err := e.svc.GetSubscription(ctx, tx, orgID)
		require.NoError(t, err)
		assert.Equal(t, billing.FreePlanCode, sub.PlanCode)
		assert.Equal(t, billing.StatusActive, sub.Status)
		assert.True(t, epoch.Equal(sub.CurrentPeriodStart))
		assert.Nil(t, sub.CurrentPeriodEnd)
		assert.Nil(t, sub.TrialEndsAt)
		assert.Nil(t, sub.GraceUntil)
		assert.Nil(t, sub.PendingPlanCode)
		assert.False(t, sub.CancelAtPeriodEnd)
	})

	ctx := reqctx.WithOrgID(context.Background(), orgID)
	err := e.db.WithTx(ctx, func(tx pgx.Tx) error { return e.svc.StartFree(ctx, tx, orgID) })
	assert.Error(t, err, "one subscription per organization")
}

func TestGetSubscription_ReturnsPlanCodesAndSchedule(t *testing.T) {
	e := newEnv(t)
	orgID := e.newOrg()
	e.subscribe(orgID, "business", billing.StatusActive, nil)
	_, err := e.admin.Exec(context.Background(), `
		UPDATE subscriptions SET cancel_at_period_end = true, pending_plan_id = (SELECT id FROM plans WHERE code = 'pro')
		WHERE organization_id = $1`, orgID)
	require.NoError(t, err)

	e.inTenant(orgID, func(ctx context.Context, tx pgx.Tx) {
		sub, err := e.svc.GetSubscription(ctx, tx, orgID)

		require.NoError(t, err)
		assert.Equal(t, "business", sub.PlanCode)
		require.NotNil(t, sub.PendingPlanCode)
		assert.Equal(t, "pro", *sub.PendingPlanCode)
		assert.True(t, sub.CancelAtPeriodEnd)
		require.NotNil(t, sub.CurrentPeriodEnd)
		assert.True(t, epoch.Add(30*24*time.Hour).Equal(*sub.CurrentPeriodEnd))
	})
}

func TestGetSubscription_WithoutOneIsNotFound(t *testing.T) {
	e := newEnv(t)
	orgID := e.newOrg()

	e.inTenant(orgID, func(ctx context.Context, tx pgx.Tx) {
		_, err := e.svc.GetSubscription(ctx, tx, orgID)

		assert.True(t, apperr.IsCode(err, apperr.CodeNotFound))
	})
}

func TestGetSubscription_AnotherTenantsSubscriptionIsInvisible(t *testing.T) {
	e := newEnv(t)
	a, b := e.newOrg(), e.newOrg()
	e.subscribe(a, "pro", billing.StatusActive, nil)
	e.subscribe(b, "business", billing.StatusActive, nil)

	e.inTenant(a, func(ctx context.Context, tx pgx.Tx) {
		_, err := e.svc.GetSubscription(ctx, tx, b)
		assert.True(t, apperr.IsCode(err, apperr.CodeNotFound), "row-level security hides it even when asked for by ID")

		plan, err := e.svc.EffectivePlan(ctx, tx, b)
		require.NoError(t, err)
		assert.Equal(t, "free", plan.Code, "and what is invisible grants nothing")
	})
}

func TestEffectivePlan_FollowsTheSubscriptionStatus(t *testing.T) {
	grace := func(d time.Duration) *time.Time { t := epoch.Add(d); return &t }
	tests := map[string]struct {
		plan   string
		status billing.Status
		grace  *time.Time
		want   string
	}{
		"free is free":                  {"free", billing.StatusActive, nil, "free"},
		"active pro":                    {"pro", billing.StatusActive, nil, "pro"},
		"trialing business":             {"business", billing.StatusTrialing, nil, "business"},
		"past_due inside the grace":     {"pro", billing.StatusPastDue, grace(time.Hour), "pro"},
		"past_due after the grace":      {"pro", billing.StatusPastDue, grace(-time.Hour), "free"},
		"past_due without a grace":      {"pro", billing.StatusPastDue, nil, "free"},
		"expired falls back to free":    {"business", billing.StatusExpired, nil, "free"},
		"canceled falls back to free":   {"business", billing.StatusCanceled, nil, "free"},
		"expired with a stale grace":    {"pro", billing.StatusExpired, grace(time.Hour), "free"},
		"the fallback carries its data": {"pro", billing.StatusExpired, nil, "free"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			orgID := e.newOrg()
			e.subscribe(orgID, tt.plan, tt.status, tt.grace)

			e.inTenant(orgID, func(ctx context.Context, tx pgx.Tx) {
				got, err := e.svc.EffectivePlan(ctx, tx, orgID)

				require.NoError(t, err)
				assert.Equal(t, tt.want, got.Code)
			})
		})
	}
}

func TestEffectivePlan_GraceEndsAsTheClockMovesOn(t *testing.T) {
	e := newEnv(t)
	orgID := e.newOrg()
	graceEnd := epoch.Add(3 * 24 * time.Hour)
	e.subscribe(orgID, "pro", billing.StatusPastDue, &graceEnd)
	codeAt := func() string {
		var code string
		e.inTenant(orgID, func(ctx context.Context, tx pgx.Tx) {
			p, err := e.svc.EffectivePlan(ctx, tx, orgID)
			require.NoError(t, err)
			code = p.Code
		})
		return code
	}

	assert.Equal(t, "pro", codeAt())
	e.clock.Set(graceEnd.Add(-time.Second))
	assert.Equal(t, "pro", codeAt())
	e.clock.Set(graceEnd)
	assert.Equal(t, "free", codeAt(), "the grace period is over at the instant it ends")
}

func TestEffectivePlan_NoSubscriptionMeansFreeAndCarriesTheCatalogueData(t *testing.T) {
	e := newEnv(t)
	orgID := e.newOrg()

	e.inTenant(orgID, func(ctx context.Context, tx pgx.Tx) {
		got, err := e.svc.EffectivePlan(ctx, tx, orgID)

		require.NoError(t, err)
		assert.Equal(t, "free", got.Code)
		assert.Equal(t, "Free", got.Name)
		assert.Equal(t, map[string]int{"members": 3}, got.Limits)
		assert.Empty(t, got.Features)
		assert.EqualValues(t, 0, got.PriceAmount)
		assert.Equal(t, "IDR", got.Currency)
	})
}

func TestEffectivePlan_ProPlanCarriesItsFeaturesAndLimits(t *testing.T) {
	e := newEnv(t)
	orgID := e.newOrg()
	e.subscribe(orgID, "pro", billing.StatusActive, nil)

	e.inTenant(orgID, func(ctx context.Context, tx pgx.Tx) {
		got, err := e.svc.EffectivePlan(ctx, tx, orgID)

		require.NoError(t, err)
		assert.Equal(t, []string{"export_csv"}, got.Features)
		assert.Equal(t, map[string]int{"members": 10}, got.Limits)
		assert.Equal(t, 14, got.TrialDays)
		assert.Equal(t, "month", got.Interval)
	})
}

func TestEffectivePlan_AMissingFreePlanIsAnError(t *testing.T) {
	e := newEnv(t)
	orgID := e.newOrg()
	_, err := e.admin.Exec(context.Background(), "DELETE FROM plans WHERE code = 'free'")
	require.NoError(t, err)

	e.inTenant(orgID, func(ctx context.Context, tx pgx.Tx) {
		_, err := e.svc.EffectivePlan(ctx, tx, orgID)

		assert.Error(t, err, "falling back to a plan that does not exist must be loud, not silently unlimited")
	})
}

func TestActivePlans_ListsTheCatalogueInDisplayOrderWithoutInactiveOnes(t *testing.T) {
	e := newEnv(t)
	_, err := e.admin.Exec(context.Background(), `
		INSERT INTO plans (code, name, price_amount, is_active, sort_order) VALUES ('legacy', 'Legacy', 5000, false, 0)`)
	require.NoError(t, err)

	plans, err := e.svc.ActivePlans(context.Background(), e.db.Pool())

	require.NoError(t, err)
	codes := make([]string, len(plans))
	for i, p := range plans {
		codes[i] = p.Code
	}
	assert.Equal(t, []string{"free", "pro", "business"}, codes)
	assert.Equal(t, []string{"export_csv", "audit_log"}, plans[2].Features)
	assert.Equal(t, -1, plans[2].Limits["members"])
}
