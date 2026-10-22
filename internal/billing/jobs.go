package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/billing/billingdb"
	"github.com/kangsolder001/saka/internal/platform/scheduler"
)

// The periodic jobs of spec §8.6. Each one is a method of Service that takes
// only a context (time comes from the injected clock) and returns what it did,
// so the scheduler in the worker can run it and tests can call it directly.
//
// All of them are idempotent and safe to run twice at once, with or without
// the scheduler's advisory lock (which only avoids duplicate work):
//
//   - rows are visited in pages and each is handled in a system transaction of
//     its own, locked FOR UPDATE SKIP LOCKED, with its conditions checked again
//     after the lock, so a row another worker is on is left to it and one
//     already handled no longer matches;
//   - what a job creates is backed by a unique index (one renewal per
//     period), or marked in the same transaction that sends the email (a
//     reminder), so repeating it creates and sends nothing new.
//
// One bad row never stops a job: it is logged, counted as failed, and tried
// again at the next run; the job's error reports them all at the end.

// Job names, as logged and as advisory lock keys.
const (
	JobRenewals       = "billing.renewals"
	JobExpire         = "billing.expire"
	JobApplyScheduled = "billing.apply_scheduled"
	JobReminders      = "billing.reminders"
	JobReconcile      = "billing.reconcile"
)

// jobPage is how many candidates a job reads at a time.
const jobPage = 100

// RegisterJobs registers the billing jobs with their intervals (hourly:
// renewals, expire, apply_scheduled; daily: reminders, reconcile).
func (s *Service) RegisterJobs(sched *scheduler.Scheduler) error {
	pay, err := s.payments()
	if err != nil {
		return err
	}
	hourly := pay.Settings.HourlyInterval
	sched.Register(scheduler.Job{Name: JobRenewals, Interval: hourly, Run: s.IssueRenewals})
	return nil
}

// sweep visits candidate ids page by page and handles each in its own system
// transaction. handle returns a short outcome name that is counted ("issued",
// "skipped"...). A handler error is logged and counted under "failed", and
// the sweep goes on; the errors are returned together when it is done.
func (s *Service) sweep(
	ctx context.Context, pay *payments, job string,
	page func(ctx context.Context, tx pgx.Tx, after uuid.UUID) ([]uuid.UUID, error),
	handle func(ctx context.Context, tx pgx.Tx, id uuid.UUID) (string, error),
) (scheduler.Counts, error) {
	counts := scheduler.Counts{}
	var failures []error
	after := uuid.Nil
	for {
		var ids []uuid.UUID
		err := pay.Tx.WithSystemTx(ctx, func(tx pgx.Tx) error {
			var err error
			ids, err = page(ctx, tx, after)
			return err
		})
		if err != nil {
			return counts, errors.Join(append(failures, fmt.Errorf("billing: %s: list candidates: %w", job, err))...)
		}
		if len(ids) == 0 {
			break
		}
		for _, id := range ids {
			if err := ctx.Err(); err != nil {
				return counts, errors.Join(append(failures, err)...)
			}
			var outcome string
			err := pay.Tx.WithSystemTx(ctx, func(tx pgx.Tx) error {
				var err error
				outcome, err = handle(ctx, tx, id)
				return err
			})
			if err != nil {
				counts["failed"]++
				failures = append(failures, fmt.Errorf("billing: %s: %s: %w", job, id, err))
				zl := pay.Log.FromContext(ctx)
				zl.Error().Err(err).Str("job", job).Str("id", id.String()).Msg("billing job could not handle a row; it is tried again at the next run")
				continue
			}
			counts[outcome]++
		}
		after = ids[len(ids)-1]
		if len(ids) < jobPage {
			break
		}
	}
	return counts, errors.Join(failures...)
}

// IssueRenewals (billing.renewals) issues the invoice for the next period of
// every paid subscription whose period ends within the renewal lead time (3
// days by default), so the owner can pay before the period ends. The invoice
// is for the plan booked in pending_plan_id when there is one (a downgrade
// applies through its renewal), with the period that starts when the current
// one ends. A subscription set to cancel gets none. Running again creates
// nothing: a period has one renewal invoice (unique index), and the row is
// checked again under its lock.
func (s *Service) IssueRenewals(ctx context.Context) (scheduler.Counts, error) {
	pay, err := s.payments()
	if err != nil {
		return nil, err
	}
	horizon := s.clk.Now().Add(pay.Settings.RenewalLead)
	return s.sweep(ctx, pay, JobRenewals,
		func(ctx context.Context, tx pgx.Tx, after uuid.UUID) ([]uuid.UUID, error) {
			return queries(tx).ListRenewalCandidates(ctx, billingdb.ListRenewalCandidatesParams{
				Horizon: horizon, AfterID: after, RowLimit: jobPage,
			})
		},
		func(ctx context.Context, tx pgx.Tx, id uuid.UUID) (string, error) {
			return s.issueRenewal(ctx, tx, pay, id, horizon)
		})
}

func (s *Service) issueRenewal(ctx context.Context, tx pgx.Tx, pay *payments, id uuid.UUID, horizon time.Time) (string, error) {
	qs := queries(tx)
	sub, err := qs.LockSubscriptionSkipLocked(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "busy", nil // another worker has it
	}
	if err != nil {
		return "", fmt.Errorf("lock subscription: %w", err)
	}
	// The conditions again, now that the row is ours.
	status := Status(sub.Status)
	if (status != StatusActive && status != StatusPastDue) || sub.CurrentPeriodEnd == nil || sub.CancelAtPeriodEnd || sub.CurrentPeriodEnd.After(horizon) {
		return "skipped", nil
	}
	periodStart := *sub.CurrentPeriodEnd
	if _, err := qs.GetRenewalInvoice(ctx, billingdb.GetRenewalInvoiceParams{SubscriptionID: sub.ID, PeriodStart: periodStart}); err == nil {
		return "exists", nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("find renewal invoice: %w", err)
	}

	planID := sub.PlanID
	if sub.PendingPlanID != nil {
		planID = *sub.PendingPlanID
	}
	plan, err := qs.GetPlanByID(ctx, planID)
	if err != nil {
		return "", fmt.Errorf("get plan: %w", err)
	}
	if plan.PriceAmount <= 0 {
		return "skipped", nil // nothing to bill
	}
	// A renewal whose link would already be dead cannot be paid: the
	// subscription is about to expire and comes back through checkout.
	if !s.clk.Now().Before(pay.linkExpiry(InvoiceKindRenewal, periodStart)) {
		return "too_late", nil
	}
	periodEnd, err := AddInterval(periodStart, plan.Interval)
	if err != nil {
		return "", fmt.Errorf("plan %q: %w", plan.Code, err)
	}
	if _, err := s.issueInvoice(ctx, tx, pay, issue{
		OrganizationID: sub.OrganizationID, SubscriptionID: sub.ID, Kind: InvoiceKindRenewal, Plan: plan,
		PeriodStart: periodStart, PeriodEnd: periodEnd, DueAt: periodStart,
	}); err != nil {
		return "", err
	}
	return "issued", nil
}
