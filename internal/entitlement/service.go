package entitlement

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/google/uuid"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/db"
)

// LimitUsage is how much of a limit an organization has used. Max is the plan's
// maximum, Unlimited (-1) for no maximum.
type LimitUsage struct {
	Used int
	Max  int
}

// Entitlements is what the rest of the application asks about an organization's
// plan (spec §9.1). Every method takes the database handle to read through, so
// a check shares the transaction of the change it guards; q must be a
// transaction pinned to the tenant (or a system transaction), never the plain
// pool, where row-level security would show nothing.
type Entitlements interface {
	// HasFeature reports whether the effective plan includes the feature.
	HasFeature(ctx context.Context, q db.DBTX, orgID uuid.UUID, feature string) (bool, error)
	// CheckLimit returns PLAN_LIMIT_REACHED when adding delta to the current
	// usage of the limit would exceed the effective plan's maximum.
	CheckLimit(ctx context.Context, q db.DBTX, orgID uuid.UUID, limit string, delta int) error
	// Usage reports the usage of every limit that has a counter, against the
	// effective plan's maximum.
	Usage(ctx context.Context, q db.DBTX, orgID uuid.UUID) (map[string]LimitUsage, error)
}

// Counter counts what an organization currently uses of one limit, from the
// real data (never from a stored counter, which could drift: spec §9.2). The
// module that owns the counted resource provides it.
type Counter func(ctx context.Context, q db.DBTX, orgID uuid.UUID) (int, error)

// Service implements Entitlements on top of a PlanSource and the registered
// counters.
type Service struct {
	plans PlanSource
	// tx opens the transaction of RequireFeature.
	tx Transactor

	mu       sync.RWMutex
	counters map[string]Counter
}

var _ Entitlements = (*Service)(nil)

// NewService creates a Service. tx is only used by the RequireFeature
// middleware. Counters are added with RegisterCounter by the composition root.
func NewService(plans PlanSource, tx Transactor) *Service {
	return &Service{plans: plans, tx: tx, counters: map[string]Counter{}}
}

// RegisterCounter makes limit checkable: fn counts its current usage. It is
// for wiring at start-up, and registering a name twice or a nil counter is a
// programming error, so it panics.
func (s *Service) RegisterCounter(limit string, fn Counter) {
	if limit == "" || fn == nil {
		panic("entitlement: RegisterCounter needs a limit name and a counter")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.counters[limit]; dup {
		panic(fmt.Sprintf("entitlement: counter for limit %q registered twice", limit))
	}
	s.counters[limit] = fn
}

func (s *Service) counter(limit string) (Counter, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fn, ok := s.counters[limit]
	return fn, ok
}

// HasFeature reports whether the effective plan of the organization includes
// the feature.
func (s *Service) HasFeature(ctx context.Context, q db.DBTX, orgID uuid.UUID, feature string) (bool, error) {
	plan, err := s.plans.EffectivePlan(ctx, q, orgID)
	if err != nil {
		return false, err
	}
	return plan.HasFeature(feature), nil
}

// CheckFeature is HasFeature for callers that want the API error: it returns
// PLAN_FEATURE_UNAVAILABLE, naming the cheapest plan that has the feature.
func (s *Service) CheckFeature(ctx context.Context, q db.DBTX, orgID uuid.UUID, feature string) error {
	plan, err := s.plans.EffectivePlan(ctx, q, orgID)
	if err != nil {
		return err
	}
	if plan.HasFeature(feature) {
		return nil
	}
	upgrade, err := s.cheapestPlan(ctx, q, plan.Code, func(p Plan) bool { return p.HasFeature(feature) })
	if err != nil {
		return err
	}
	return apperr.PlanFeatureUnavailable(
		fmt.Sprintf("Fitur ini tidak tersedia di paket %s.", plan.Name), feature, plan.Code, upgrade)
}

// CheckLimit fails with PLAN_LIMIT_REACHED when the organization's usage of
// limit plus delta would exceed the effective plan's maximum. The error says
// which plan the organization is on and the cheapest plan that would allow the
// change, for an upgrade prompt.
//
// A limit the plan does not list is 0 (fail closed), and a limit without a
// counter is an error: both mean the wiring or the plan data is wrong, and
// silently allowing the change would hide it. A delta of 0 or less (removing
// things) is never refused, even when the organization is over the limit after
// a downgrade. Unlimited plans skip counting. The count and the resource insert
// must share q's transaction, and callers that can race (invitations) lock the
// organization first; this method does not lock anything itself.
func (s *Service) CheckLimit(ctx context.Context, q db.DBTX, orgID uuid.UUID, limit string, delta int) error {
	count, ok := s.counter(limit)
	if !ok {
		return fmt.Errorf("entitlement: no counter registered for limit %q", limit)
	}
	if delta <= 0 {
		return nil
	}
	plan, err := s.plans.EffectivePlan(ctx, q, orgID)
	if err != nil {
		return err
	}
	maxAllowed := plan.Limit(limit)
	if maxAllowed == Unlimited {
		return nil
	}
	used, err := count(ctx, q, orgID)
	if err != nil {
		return fmt.Errorf("entitlement: count %q: %w", limit, err)
	}
	if used+delta <= maxAllowed {
		return nil
	}
	upgrade, err := s.cheapestPlan(ctx, q, plan.Code, func(p Plan) bool { return fits(p, limit, used+delta) })
	if err != nil {
		return err
	}
	return apperr.PlanLimitReached(limitMessage(plan.Name, limit, maxAllowed), limit, maxAllowed, used, plan.Code, upgrade)
}

// Usage reports the usage of every limit with a counter against the effective
// plan's maximum (0 for a limit the plan does not list).
func (s *Service) Usage(ctx context.Context, q db.DBTX, orgID uuid.UUID) (map[string]LimitUsage, error) {
	plan, err := s.plans.EffectivePlan(ctx, q, orgID)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	counters := make(map[string]Counter, len(s.counters))
	for name, fn := range s.counters {
		counters[name] = fn
	}
	s.mu.RUnlock()

	out := make(map[string]LimitUsage, len(counters))
	for name, count := range counters {
		used, err := count(ctx, q, orgID)
		if err != nil {
			return nil, fmt.Errorf("entitlement: count %q: %w", name, err)
		}
		out[name] = LimitUsage{Used: used, Max: plan.Limit(name)}
	}
	return out, nil
}

// fits reports whether n of the limit is within the plan.
func fits(p Plan, limit string, n int) bool {
	maxAllowed := p.Limit(limit)
	return maxAllowed == Unlimited || n <= maxAllowed
}

// cheapestPlan returns the code of the cheapest active plan, other than the
// current one, that satisfies ok, or nil when there is none. Ties keep the
// catalogue's display order.
func (s *Service) cheapestPlan(ctx context.Context, q db.DBTX, currentCode string, ok func(Plan) bool) (*string, error) {
	plans, err := s.plans.ActivePlans(ctx, q)
	if err != nil {
		return nil, err
	}
	var candidates []Plan
	for _, p := range plans {
		if p.Code != currentCode && ok(p) {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) == 0 {
		return nil, nil //nolint:nilnil // no upgrade is a valid answer, not an error
	}
	best := slices.MinFunc(candidates, func(a, b Plan) int {
		switch {
		case a.PriceAmount < b.PriceAmount:
			return -1
		case a.PriceAmount > b.PriceAmount:
			return 1
		}
		return 0
	})
	return &best.Code, nil
}

// limitLabels name what a limit counts, for the message shown to users.
var limitLabels = map[string]string{
	"members": "anggota",
}

func limitMessage(planName, limit string, maxAllowed int) string {
	label := limit
	if l, ok := limitLabels[limit]; ok {
		label = l
	}
	if maxAllowed == 0 {
		return fmt.Sprintf("Paket %s tidak mendukung %s.", planName, label)
	}
	return fmt.Sprintf("Paket %s hanya mendukung %d %s.", planName, maxAllowed, label)
}
