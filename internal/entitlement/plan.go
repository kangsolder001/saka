// Package entitlement decides what an organization may use: which features its
// plan includes and how much of each limit it has left (spec §9). It is a thin
// policy layer. Plans and subscriptions belong to billing, usage counts to the
// modules that own the counted resources; this package only joins them, so it
// imports neither.
package entitlement

import (
	"context"
	"slices"

	"github.com/google/uuid"

	"github.com/kangsolder001/saka/internal/platform/db"
)

// Unlimited is the limit value that means "no maximum" (plans.limits).
const Unlimited = -1

// Plan is a plan as the entitlement checks and the public catalogue see it.
type Plan struct {
	Code string
	Name string
	// PriceAmount is in the smallest currency unit (IDR has no decimals).
	PriceAmount int64
	Currency    string
	Interval    string
	TrialDays   int
	Features    []string
	// Limits maps a limit name to its maximum; Unlimited (-1) means no maximum.
	Limits map[string]int
}

// HasFeature reports whether the plan includes the feature.
func (p Plan) HasFeature(feature string) bool {
	return slices.Contains(p.Features, feature)
}

// Limit returns the maximum of a limit. A limit the plan does not mention is 0,
// not unlimited: forgetting to configure a new limit must fail closed.
func (p Plan) Limit(name string) int {
	return p.Limits[name]
}

// PlanSource is what the entitlement checks need from billing
// (*billing.Service implements it).
type PlanSource interface {
	// EffectivePlan returns the plan the organization currently has access to:
	// its subscription's plan while the subscription grants access, otherwise
	// the Free plan (spec §9.2). q must be a transaction pinned to the tenant.
	EffectivePlan(ctx context.Context, q db.DBTX, orgID uuid.UUID) (Plan, error)
	// ActivePlans returns the plans offered for sale, in display order.
	ActivePlans(ctx context.Context, q db.DBTX) ([]Plan, error)
}
