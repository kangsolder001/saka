package billing

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/billing/billingdb"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/db"
)

// LimitWarning says an organization uses more of a limit than a plan it is
// moving to allows. It is only a warning: data is never deleted (spec §8.2),
// but creating more of it is blocked once the smaller plan applies (§8.4).
type LimitWarning struct {
	Limit string
	Used  int
	// Max is what the new plan allows.
	Max int
}

// ChangePlanResult is what a plan change did.
type ChangePlanResult struct {
	// Checkout is set when the change has to be paid before it applies (an
	// upgrade, or switching plans on a trial or a past due subscription): the
	// invoice to pay, exactly as a checkout returns it.
	Checkout *CheckoutResult
	// Scheduled is true for a downgrade booked for the end of the period.
	Scheduled bool
	// Cleared is true when asking for the current plan took back a scheduled downgrade.
	Cleared bool
	// Warnings lists the limits a scheduled downgrade would leave exceeded.
	Warnings []LimitWarning
}

// ChangePlan moves the current organization to another paid plan (spec §8.4,
// no proration).
//
//   - Upgrade (a dearer plan, or any plan from Free): same as a checkout. An
//     invoice is created, and the plan changes, with a period starting then,
//     only once it is paid.
//   - Downgrade (a cheaper plan on an active paid subscription): nothing is
//     paid and nothing changes now. The plan is booked in pending_plan_id and
//     applies when the period ends, through the renewal invoice for it. The
//     result warns about limits the smaller plan would leave exceeded.
//   - Asking for the plan the subscription is on takes back a scheduled
//     downgrade, and is a conflict when none is scheduled.
//   - A trial or past due subscription has no period to wait for: any other
//     plan is simply bought (an invoice), like a checkout.
//   - An expired or canceled subscription cannot change plan: it subscribes
//     again through checkout.
//
// Owners only.
func (s *Service) ChangePlan(ctx context.Context, planCode string) (ChangePlanResult, error) {
	orgID, err := billingTenant(ctx)
	if err != nil {
		return ChangePlanResult{}, err
	}
	pay, err := s.payments()
	if err != nil {
		return ChangePlanResult{}, err
	}
	var out ChangePlanResult
	err = pay.Tx.WithTx(ctx, func(tx pgx.Tx) error {
		plan, err := s.purchasablePlan(ctx, tx, planCode)
		if err != nil {
			return err
		}
		sub, err := lockSubscription(ctx, tx, orgID)
		if err != nil {
			return err
		}
		out, err = s.changePlan(ctx, tx, pay, sub, plan)
		return err
	})
	return out, err
}

func (s *Service) changePlan(ctx context.Context, tx pgx.Tx, pay *payments, sub billingdb.Subscription, plan billingdb.Plan) (ChangePlanResult, error) {
	status := Status(sub.Status)
	snap := snapshotOf(sub)
	qs := queries(tx)

	buy := func() (ChangePlanResult, error) {
		res, err := s.startPurchase(ctx, tx, pay, sub, plan)
		if err != nil {
			return ChangePlanResult{}, err
		}
		return ChangePlanResult{Checkout: &res}, nil
	}

	switch {
	case status == StatusExpired || status == StatusCanceled:
		return ChangePlanResult{}, apperr.Conflict("The subscription has ended. Subscribe again through checkout.").
			WithDetails(map[string]any{"reason": "use_checkout"})
	case !renewing(snap):
		return buy() // Free: any paid plan is an upgrade
	case sub.PlanID == plan.ID:
		if status == StatusActive {
			if sub.PendingPlanID == nil {
				return ChangePlanResult{}, apperr.Conflict("The organization is already subscribed to this plan.").
					WithDetails(map[string]any{"reason": "already_subscribed"})
			}
			if err := s.saveSnapshot(ctx, tx, sub.ID, CancelDowngrade(snap)); err != nil {
				return ChangePlanResult{}, err
			}
			// A renewal invoice already issued was for the plan being dropped.
			if err := s.voidRenewals(ctx, tx, sub.ID); err != nil {
				return ChangePlanResult{}, err
			}
			return ChangePlanResult{Cleared: true}, nil
		}
		return buy()
	case status != StatusActive:
		return buy() // trial or past due: buying is the only way to switch
	}

	current, err := qs.GetPlanByID(ctx, sub.PlanID)
	if err != nil {
		return ChangePlanResult{}, fmt.Errorf("billing: get current plan: %w", err)
	}
	if plan.PriceAmount > current.PriceAmount {
		return buy()
	}

	next, err := ScheduleDowngrade(snap, plan.ID)
	if err != nil {
		return ChangePlanResult{}, fmt.Errorf("billing: schedule downgrade: %w", err)
	}
	if err := s.saveSnapshot(ctx, tx, sub.ID, next); err != nil {
		return ChangePlanResult{}, err
	}
	// The renewal invoice for the old plan, if one was already issued, is
	// replaced: the renewals job issues it again for the plan booked now.
	if err := s.voidRenewals(ctx, tx, sub.ID); err != nil {
		return ChangePlanResult{}, err
	}
	warnings, err := s.downgradeWarnings(ctx, tx, pay, sub, plan)
	if err != nil {
		return ChangePlanResult{}, err
	}
	return ChangePlanResult{Scheduled: true, Warnings: warnings}, nil
}

// voidRenewals closes the renewal invoices still waiting for payment.
func (s *Service) voidRenewals(ctx context.Context, q db.DBTX, subscriptionID uuid.UUID) error {
	if _, err := queries(q).VoidLiveInvoices(ctx, billingdb.VoidLiveInvoicesParams{
		SubscriptionID: subscriptionID, Kind: ptr(string(InvoiceKindRenewal)), Now: s.clk.Now(),
	}); err != nil {
		return fmt.Errorf("billing: void renewal invoices: %w", err)
	}
	return nil
}

// downgradeWarnings compares what the organization uses now with the limits of
// the plan it is moving to. A limit the plan does not list counts as zero, as
// everywhere else in entitlement (fail closed).
func (s *Service) downgradeWarnings(ctx context.Context, q db.DBTX, pay *payments, sub billingdb.Subscription, to billingdb.Plan) ([]LimitWarning, error) {
	if pay.Usage == nil {
		return nil, nil
	}
	target, err := planFromRow(to)
	if err != nil {
		return nil, fmt.Errorf("billing: %w", err)
	}
	usage, err := pay.Usage.Usage(ctx, q, sub.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("billing: read usage: %w", err)
	}
	var warnings []LimitWarning
	for name, u := range usage {
		max := target.Limits[name]
		if max != -1 && u.Used > max {
			warnings = append(warnings, LimitWarning{Limit: name, Used: u.Used, Max: max})
		}
	}
	sort.Slice(warnings, func(i, j int) bool { return warnings[i].Limit < warnings[j].Limit })
	return warnings, nil
}

// CancelSubscription sets the current organization's subscription to end when
// its paid period does (spec §8.3): access continues until then and nothing is
// refunded. Asking again changes nothing. Renewal invoices still waiting for
// payment are voided, so no payment extends a subscription that is ending.
// Owners only; a Free, expired or canceled subscription has nothing to cancel
// (409).
func (s *Service) CancelSubscription(ctx context.Context) error {
	return s.setCancellation(ctx, RequestCancellation, true)
}

// ResumeSubscription takes back a cancellation before the period ends. The
// renewal invoice is issued by the renewals job when its time comes. Owners
// only; a Free, expired or canceled subscription has nothing to resume (409).
func (s *Service) ResumeSubscription(ctx context.Context) error {
	return s.setCancellation(ctx, RevokeCancellation, false)
}

func (s *Service) setCancellation(ctx context.Context, rule func(Snapshot) (Snapshot, error), cancel bool) error {
	orgID, err := billingTenant(ctx)
	if err != nil {
		return err
	}
	pay, err := s.payments()
	if err != nil {
		return err
	}
	return pay.Tx.WithTx(ctx, func(tx pgx.Tx) error {
		sub, err := lockSubscription(ctx, tx, orgID)
		if err != nil {
			return err
		}
		next, err := rule(snapshotOf(sub))
		if errors.Is(err, ErrNotRenewing) {
			reason, msg := "nothing_to_resume", "There is no canceled paid subscription to resume."
			if cancel {
				reason, msg = "nothing_to_cancel", "There is no paid subscription to cancel."
			}
			return apperr.Conflict(msg).WithDetails(map[string]any{"reason": reason})
		}
		if err != nil {
			return err
		}
		if next.CancelAtPeriodEnd == sub.CancelAtPeriodEnd {
			return nil // already so
		}
		if err := s.saveSnapshot(ctx, tx, sub.ID, next); err != nil {
			return err
		}
		if !cancel {
			return nil
		}
		return s.voidRenewals(ctx, tx, sub.ID)
	})
}
