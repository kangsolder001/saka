package billing

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/billing/billingdb"
	"github.com/kangsolder001/saka/internal/platform/scheduler"
)

// dueTransition locks a subscription that appeared in the due list and works
// out, with the state machine, what its due event does right now. Both
// billing.expire and billing.apply_scheduled start here and act only on the
// outcomes that are theirs; because Next decides by fixed priorities, a row
// is never acted on by both, and whichever runs first, the result is the same.
//
// ok is false when there is nothing to do: another worker has the row, the
// event is no longer (or not yet) due once the row is ours, or the state
// machine refuses it.
type dueResult struct {
	sub    billingdb.Subscription
	result Result
	ok     bool
	// outcome names why not, when ok is false ("busy", "skipped", "stale").
	outcome string
}

func (s *Service) dueTransition(ctx context.Context, tx pgx.Tx, pay *payments, id uuid.UUID) (dueResult, error) {
	sub, err := queries(tx).LockSubscriptionSkipLocked(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return dueResult{outcome: "busy"}, nil
	}
	if err != nil {
		return dueResult{}, fmt.Errorf("lock subscription: %w", err)
	}
	paid, err := s.paidRenewalFor(ctx, tx, sub)
	if err != nil {
		return dueResult{}, err
	}
	event, due := DueEvent(snapshotOf(sub), paid, pay.Settings.Grace, s.clk.Now())
	if !due {
		return dueResult{sub: sub, outcome: "skipped"}, nil
	}
	res, err := Next(snapshotOf(sub), event, s.clk.Now())
	switch {
	case err == nil:
		return dueResult{sub: sub, result: res, ok: true}, nil
	case errors.Is(err, ErrNotDue), errors.Is(err, ErrIllegalTransition):
		return dueResult{sub: sub, outcome: "skipped"}, nil
	case errors.Is(err, ErrStaleInvoice):
		// A paid renewal that no longer matches the period: a person looks, and
		// the subscription is left as it is rather than guessed at.
		zl := pay.Log.FromContext(ctx)
		zl.Error().Str("subscription_id", sub.ID.String()).Msg("a paid renewal does not match the subscription period; left for a person")
		return dueResult{sub: sub, outcome: "stale"}, nil
	}
	return dueResult{}, fmt.Errorf("apply %s: %w", event.Kind, err)
}

// ExpireSubscriptions (billing.expire) moves subscriptions whose time ran out
// forward and ends what nobody can pay any more (spec §8.2, §8.6):
//
//   - a trial that ended unpaid: trialing → expired;
//   - a paid period that ended without a paid renewal: active → past_due, with
//     access kept until the grace period (counted from the period end) is over,
//     and an email with the payment link;
//   - a grace period that ended: past_due → expired, the renewal invoice
//     expires with it, and the owner is told.
//
// Then invoices past the time they could be paid are expired. A period that
// ended with the next one paid, or with a cancellation pending, is not this
// job's: billing.apply_scheduled does those. The end of a grace period is exact
// whether or not this job is on time (entitlement compares grace_until with the
// clock itself); what waits for the job is the move from active to past_due, so
// access can run on for up to one job interval past a period end.
func (s *Service) ExpireSubscriptions(ctx context.Context) (scheduler.Counts, error) {
	pay, err := s.payments()
	if err != nil {
		return nil, err
	}
	counts, err := s.sweepDue(ctx, pay, JobExpire, func(ctx context.Context, tx pgx.Tx, d dueResult) (string, error) {
		switch d.result.Outcome {
		case OutcomePastDue:
			return s.makePastDue(ctx, tx, pay, d)
		case OutcomeExpired:
			return s.makeExpired(ctx, tx, pay, d)
		}
		return "skipped", nil // renewed or canceled: billing.apply_scheduled
	})

	// Invoices nobody can pay any more, whatever became of their subscription.
	now := s.clk.Now()
	var swept int64
	sweepErr := pay.Tx.WithSystemTx(ctx, func(tx pgx.Tx) error {
		var err error
		swept, err = queries(tx).ExpireOverdueInvoices(ctx, billingdb.ExpireOverdueInvoicesParams{
			Now: now, RenewalCutoff: now.Add(-pay.Settings.Grace),
		})
		return err
	})
	if sweepErr != nil {
		sweepErr = fmt.Errorf("billing: %s: expire overdue invoices: %w", JobExpire, sweepErr)
	}
	if swept > 0 {
		counts["invoices_expired"] = int(swept)
	}
	return counts, errors.Join(err, sweepErr)
}

// sweepDue is sweep over the due list, handing each locked, due subscription
// to act. Rows that are not due after all are counted by the reason.
func (s *Service) sweepDue(ctx context.Context, pay *payments, job string, act func(ctx context.Context, tx pgx.Tx, d dueResult) (string, error)) (scheduler.Counts, error) {
	now := s.clk.Now()
	return s.sweep(ctx, pay, job,
		func(ctx context.Context, tx pgx.Tx, after uuid.UUID) ([]uuid.UUID, error) {
			return queries(tx).ListDueSubscriptions(ctx, billingdb.ListDueSubscriptionsParams{Now: now, AfterID: after, RowLimit: jobPage})
		},
		func(ctx context.Context, tx pgx.Tx, id uuid.UUID) (string, error) {
			d, err := s.dueTransition(ctx, tx, pay, id)
			if err != nil {
				return "", err
			}
			if !d.ok {
				return d.outcome, nil
			}
			return act(ctx, tx, d)
		})
}

// makePastDue applies active → past_due and tells the owner, with the payment
// link of the renewal invoice when there is one.
func (s *Service) makePastDue(ctx context.Context, tx pgx.Tx, pay *payments, d dueResult) (string, error) {
	if err := s.saveSnapshot(ctx, tx, d.sub.ID, d.result.Snapshot); err != nil {
		return "", err
	}
	qs := queries(tx)
	plan, err := qs.GetPlanByID(ctx, d.sub.PlanID)
	if err != nil {
		return "", fmt.Errorf("get plan: %w", err)
	}
	link, err := s.livePaymentURL(ctx, tx, d.sub.ID, InvoiceKindRenewal)
	if err != nil {
		return "", err
	}
	contact, ok, err := pay.contactOf(ctx, tx, d.sub.OrganizationID)
	if err != nil {
		return "", err
	}
	if ok {
		if err := pay.sendEmail(ctx, tx, contact.OwnerEmail, "subscription_past_due", map[string]any{
			"name": contact.OwnerName, "organization_name": contact.OrganizationName, "plan_name": plan.Name,
			"grace_until": FormatDate(*d.result.Snapshot.GraceUntil), "payment_url": link,
		}); err != nil {
			return "", err
		}
	}
	return "past_due", nil
}

// makeExpired applies trialing/past_due → expired, expires the renewal invoice
// that can no longer save the subscription, and tells the owner.
func (s *Service) makeExpired(ctx context.Context, tx pgx.Tx, pay *payments, d dueResult) (string, error) {
	if err := s.saveSnapshot(ctx, tx, d.sub.ID, d.result.Snapshot); err != nil {
		return "", err
	}
	qs := queries(tx)
	if _, err := qs.ExpireLiveInvoices(ctx, billingdb.ExpireLiveInvoicesParams{
		SubscriptionID: d.sub.ID, Kind: ptr(string(InvoiceKindRenewal)), Now: s.clk.Now(),
	}); err != nil {
		return "", fmt.Errorf("expire renewal invoices: %w", err)
	}
	plan, err := qs.GetPlanByID(ctx, d.sub.PlanID)
	if err != nil {
		return "", fmt.Errorf("get plan: %w", err)
	}
	contact, ok, err := pay.contactOf(ctx, tx, d.sub.OrganizationID)
	if err != nil {
		return "", err
	}
	if ok {
		if err := pay.sendEmail(ctx, tx, contact.OwnerEmail, "subscription_expired", map[string]any{
			"name": contact.OwnerName, "organization_name": contact.OrganizationName, "plan_name": plan.Name,
		}); err != nil {
			return "", err
		}
	}
	return "expired", nil
}

// livePaymentURL is the payment link of the subscription's open invoice of a
// kind, or "" when there is none.
func (s *Service) livePaymentURL(ctx context.Context, tx pgx.Tx, subscriptionID uuid.UUID, kind InvoiceKind) (string, error) {
	live, err := queries(tx).LockLiveInvoicesForSubscription(ctx, subscriptionID)
	if err != nil {
		return "", fmt.Errorf("find open invoices: %w", err)
	}
	for _, inv := range live {
		if InvoiceKind(inv.Kind) == kind && InvoiceStatus(inv.Status) == InvoiceOpen && inv.PaymentUrl != nil {
			return *inv.PaymentUrl, nil
		}
	}
	return "", nil
}

// ApplyScheduled (billing.apply_scheduled) applies what was booked for the end
// of a paid period once it has ended (spec §8.4, §8.6):
//
//   - cancel_at_period_end: the subscription becomes canceled (access ends
//     now; nothing is refunded);
//   - a paid renewal for the next period: the subscription rolls into it, on
//     the plan that renewal was billed for, so a downgrade booked in
//     pending_plan_id takes effect here, through its renewal invoice.
//
// A period that ended with neither is billing.expire's (past due).
func (s *Service) ApplyScheduled(ctx context.Context) (scheduler.Counts, error) {
	pay, err := s.payments()
	if err != nil {
		return nil, err
	}
	return s.sweepDue(ctx, pay, JobApplyScheduled, func(ctx context.Context, tx pgx.Tx, d dueResult) (string, error) {
		switch d.result.Outcome {
		case OutcomeRenewed:
			if err := s.saveSnapshot(ctx, tx, d.sub.ID, d.result.Snapshot); err != nil {
				return "", err
			}
			return "renewed", nil
		case OutcomeCanceled:
			if err := s.saveSnapshot(ctx, tx, d.sub.ID, d.result.Snapshot); err != nil {
				return "", err
			}
			// A renewal still waiting for payment has no use any more.
			if _, err := queries(tx).VoidLiveInvoices(ctx, billingdb.VoidLiveInvoicesParams{
				SubscriptionID: d.sub.ID, Kind: ptr(string(InvoiceKindRenewal)), Now: s.clk.Now(),
			}); err != nil {
				return "", fmt.Errorf("void renewal invoices: %w", err)
			}
			return "canceled", nil
		}
		return "skipped", nil // past due or expired: billing.expire
	})
}
