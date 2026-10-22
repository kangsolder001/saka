// Package billing owns plans, subscriptions, invoices and the payment
// providers (spec §8). The organization is what subscribes, never a user.
// The subscription state machine is a set of pure functions
// (statemachine.go); the rest applies what they decide to the database,
// inside the caller's transaction.
package billing

import (
	"time"

	"github.com/google/uuid"

	"github.com/kangsolder001/saka/internal/billing/billingdb"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/db"
)

// FreePlanCode is the plan every organization starts on and falls back to when
// its subscription stops granting access.
const FreePlanCode = "free"

// Status is the state of a subscription (spec §8.2).
type Status string

const (
	StatusTrialing Status = "trialing"
	StatusActive   Status = "active"
	StatusPastDue  Status = "past_due"
	StatusExpired  Status = "expired"
	StatusCanceled Status = "canceled"
)

// Subscription is the organization's subscription, with plans named by code.
type Subscription struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	PlanCode       string
	Status         Status
	TrialEndsAt    *time.Time
	// CurrentPeriodEnd is nil for a period that never ends (Free).
	CurrentPeriodStart time.Time
	CurrentPeriodEnd   *time.Time
	CancelAtPeriodEnd  bool
	// PendingPlanCode is a scheduled downgrade, applied at the period end.
	PendingPlanCode *string
	GraceUntil      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func subscriptionFromRow(r billingdb.GetSubscriptionByOrganizationRow) Subscription {
	return Subscription{
		ID: r.ID, OrganizationID: r.OrganizationID, PlanCode: r.PlanCode, Status: Status(r.Status),
		TrialEndsAt: r.TrialEndsAt, CurrentPeriodStart: r.CurrentPeriodStart, CurrentPeriodEnd: r.CurrentPeriodEnd,
		CancelAtPeriodEnd: r.CancelAtPeriodEnd, PendingPlanCode: r.PendingPlanCode, GraceUntil: r.GraceUntil,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// Service is the public API of the billing module. Without options it only
// has the read side every organization needs (plans, subscriptions, the Free
// subscription); EnablePayments adds checkout, invoices, webhooks and the jobs.
type Service struct {
	clk clock.Clock
	pay *payments
}

// NewService creates a Service.
func NewService(clk clock.Clock) *Service {
	return &Service{clk: clk}
}

func queries(q db.DBTX) *billingdb.Queries { return billingdb.New(q) }
