package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/billing/billingdb"
	"github.com/kangsolder001/saka/internal/entitlement"
	"github.com/kangsolder001/saka/internal/platform/db"
)

var _ entitlement.PlanSource = (*Service)(nil)

func planFromRow(r billingdb.Plan) (entitlement.Plan, error) {
	p := entitlement.Plan{
		Code: r.Code, Name: r.Name, PriceAmount: r.PriceAmount, Currency: r.Currency,
		Interval: r.Interval, TrialDays: int(r.TrialDays),
	}
	if err := json.Unmarshal(r.Features, &p.Features); err != nil {
		return entitlement.Plan{}, fmt.Errorf("plan %q features: %w", r.Code, err)
	}
	if err := json.Unmarshal(r.Limits, &p.Limits); err != nil {
		return entitlement.Plan{}, fmt.Errorf("plan %q limits: %w", r.Code, err)
	}
	return p, nil
}

// ActivePlans returns the plans offered for sale, in display order
// (plans.sort_order).
func (s *Service) ActivePlans(ctx context.Context, q db.DBTX) ([]entitlement.Plan, error) {
	rows, err := queries(q).ListActivePlans(ctx)
	if err != nil {
		return nil, fmt.Errorf("billing: list active plans: %w", err)
	}
	plans := make([]entitlement.Plan, 0, len(rows))
	for _, r := range rows {
		p, err := planFromRow(r)
		if err != nil {
			return nil, fmt.Errorf("billing: %w", err)
		}
		plans = append(plans, p)
	}
	return plans, nil
}

// EffectivePlan returns the plan the organization has access to right now: the
// subscription's plan while its status grants access (GrantsAccess), otherwise
// the Free plan. An organization with no subscription row is on Free too. Data
// is never touched when access lapses; only what the organization may use
// changes (spec §8.2).
func (s *Service) EffectivePlan(ctx context.Context, q db.DBTX, orgID uuid.UUID) (entitlement.Plan, error) {
	qs := queries(q)
	sub, err := qs.GetSubscriptionByOrganization(ctx, orgID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return s.planByCode(ctx, qs, FreePlanCode)
	case err != nil:
		return entitlement.Plan{}, fmt.Errorf("billing: get subscription: %w", err)
	}
	if !GrantsAccess(Status(sub.Status), sub.GraceUntil, s.clk.Now()) {
		return s.planByCode(ctx, qs, FreePlanCode)
	}
	row, err := qs.GetPlanByID(ctx, sub.PlanID)
	if err != nil {
		return entitlement.Plan{}, fmt.Errorf("billing: get plan of subscription: %w", err)
	}
	p, err := planFromRow(row)
	if err != nil {
		return entitlement.Plan{}, fmt.Errorf("billing: %w", err)
	}
	return p, nil
}

func (s *Service) planByCode(ctx context.Context, qs *billingdb.Queries, code string) (entitlement.Plan, error) {
	row, err := qs.GetPlanByCode(ctx, code)
	if err != nil {
		return entitlement.Plan{}, fmt.Errorf("billing: get plan %q: %w", code, err)
	}
	p, err := planFromRow(row)
	if err != nil {
		return entitlement.Plan{}, fmt.Errorf("billing: %w", err)
	}
	return p, nil
}
