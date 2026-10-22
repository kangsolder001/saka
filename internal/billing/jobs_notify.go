package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/billing/billingdb"
	"github.com/kangsolder001/saka/internal/billing/payment"
	"github.com/kangsolder001/saka/internal/platform/scheduler"
)

// SendReminders (billing.reminders) tells the owner that an open invoice falls
// due soon: once when it is 3 days away and once when it is 1 day away (spec
// §8.6). For the trial's first invoice the email is trial_ending, because that
// due date is the end of the trial; for every other invoice it is
// payment_reminder.
//
// A reminder is recorded on the invoice in the same transaction that queues
// its email, so running the job again, or two workers at once, sends nothing
// twice. An invoice already inside a window when it was created counts as
// notified for it (see reminderMarks), and a worker that was down and finds an
// invoice inside the 1 day window sends that one email for both. Nothing is
// sent for an invoice that is paid, closed or past due, nor for a subscription
// that no longer runs.
func (s *Service) SendReminders(ctx context.Context) (scheduler.Counts, error) {
	pay, err := s.payments()
	if err != nil {
		return nil, err
	}
	now := s.clk.Now()
	return s.sweep(ctx, pay, JobReminders,
		func(ctx context.Context, tx pgx.Tx, after uuid.UUID) ([]uuid.UUID, error) {
			return queries(tx).ListReminderCandidates(ctx, billingdb.ListReminderCandidatesParams{
				Now: now, AfterID: after, RowLimit: jobPage, Horizon1d: now.Add(day), Horizon3d: now.Add(3 * day),
			})
		},
		func(ctx context.Context, tx pgx.Tx, id uuid.UUID) (string, error) {
			return s.sendReminder(ctx, tx, pay, id)
		})
}

func (s *Service) sendReminder(ctx context.Context, tx pgx.Tx, pay *payments, id uuid.UUID) (string, error) {
	qs := queries(tx)
	inv, err := qs.LockInvoiceSkipLocked(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "busy", nil
	}
	if err != nil {
		return "", fmt.Errorf("lock invoice: %w", err)
	}
	now := s.clk.Now()
	remaining := inv.DueAt.Sub(now)
	if InvoiceStatus(inv.Status) != InvoiceOpen || remaining <= 0 || inv.PaymentUrl == nil {
		return "skipped", nil
	}
	// Which window, again after the lock: the final one wins over the first.
	final := remaining <= day && inv.Reminder1dSentAt == nil
	first := remaining <= 3*day && inv.Reminder3dSentAt == nil
	if !final && !first {
		return "skipped", nil
	}

	sub, err := qs.GetSubscriptionByID(ctx, inv.SubscriptionID)
	if err != nil {
		return "", fmt.Errorf("get subscription: %w", err)
	}
	switch Status(sub.Status) {
	case StatusTrialing, StatusActive, StatusPastDue:
	default:
		return "skipped", nil // a subscription that no longer runs is not chased
	}
	plan, err := qs.GetPlanByID(ctx, inv.PlanID)
	if err != nil {
		return "", fmt.Errorf("get plan: %w", err)
	}
	contact, ok, err := pay.contactOf(ctx, tx, inv.OrganizationID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "no_contact", nil
	}

	daysLeft := int((remaining + day - 1) / day) // whole days, rounded up: 2.5 days left reads "3"
	data := invoiceEmailData(contact, inv, plan)
	data["days_left"] = daysLeft
	data["payment_url"] = *inv.PaymentUrl
	template := "payment_reminder"
	// The first invoice of a trial falls due when the trial ends.
	if Status(sub.Status) == StatusTrialing && InvoiceKind(inv.Kind) == InvoiceKindCheckout && sub.TrialEndsAt != nil && sub.TrialEndsAt.Equal(inv.DueAt) {
		template = "trial_ending"
		data["trial_end_date"] = FormatDate(inv.DueAt)
	}
	if err := pay.sendEmail(ctx, tx, contact.OwnerEmail, template, data); err != nil {
		return "", err
	}
	if err := qs.MarkReminderSent(ctx, billingdb.MarkReminderSentParams{ID: inv.ID, Now: now, Final: final}); err != nil {
		return "", fmt.Errorf("mark reminder sent: %w", err)
	}
	return "sent", nil
}

// Reconcile (billing.reconcile) asks the gateway about every invoice that is
// still open after ReconcileMinAge, and applies what it answers exactly as if
// the webhook had arrived. That repairs a lost webhook, and an event the
// gateway gave up retrying. It reuses the webhook path: the answer becomes an
// event with an ID derived from the invoice and the state, stored in
// payment_events and applied under the same locks, so running it twice, at the
// same time as a late real webhook, or on two workers changes nothing twice.
//
// A payment the gateway does not know (the customer never started it) and one
// still pending are left alone. Only paid, expired and canceled answers act;
// nothing is taken from the answer that a webhook would not be trusted with
// (amount and currency are checked against the invoice).
func (s *Service) Reconcile(ctx context.Context) (scheduler.Counts, error) {
	pay, err := s.payments()
	if err != nil {
		return nil, err
	}
	before := s.clk.Now().Add(-pay.Settings.ReconcileMinAge)
	counts := scheduler.Counts{}
	var failures []error
	after := uuid.Nil
	for {
		var rows []billingdb.ListInvoicesToReconcileRow
		err := pay.Tx.WithSystemTx(ctx, func(tx pgx.Tx) error {
			var err error
			rows, err = queries(tx).ListInvoicesToReconcile(ctx, billingdb.ListInvoicesToReconcileParams{
				CreatedBefore: before, AfterID: after, RowLimit: jobPage,
			})
			return err
		})
		if err != nil {
			return counts, errors.Join(append(failures, fmt.Errorf("billing: %s: list invoices: %w", JobReconcile, err))...)
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			if err := ctx.Err(); err != nil {
				return counts, errors.Join(append(failures, err)...)
			}
			outcome, err := s.reconcileInvoice(ctx, pay, row.Provider, row.Number)
			if err != nil {
				counts["failed"]++
				failures = append(failures, fmt.Errorf("billing: %s: %s: %w", JobReconcile, row.Number, err))
				zl := pay.Log.FromContext(ctx)
				zl.Error().Err(err).Str("job", JobReconcile).Str("invoice_number", row.Number).Msg("billing job could not reconcile an invoice; it is tried again at the next run")
				continue
			}
			counts[outcome]++
		}
		after = rows[len(rows)-1].ID
		if len(rows) < jobPage {
			break
		}
	}
	return counts, errors.Join(failures...)
}

// reconcileInvoice asks the provider of one invoice about it and feeds a
// decisive answer through the webhook path.
func (s *Service) reconcileInvoice(ctx context.Context, pay *payments, providerName, number string) (string, error) {
	provider, ok := pay.Providers[providerName]
	if !ok {
		return "unknown_provider", nil // another deployment's invoice
	}
	st, err := provider.GetPaymentStatus(ctx, number)
	if errors.Is(err, payment.ErrNotFound) {
		return "not_started", nil
	}
	if err != nil {
		return "", err
	}
	switch st.State {
	case payment.StatePaid, payment.StateExpired, payment.StateCanceled:
	default:
		return "pending", nil
	}
	raw := st.Raw
	if !json.Valid(raw) {
		raw, _ = json.Marshal(map[string]string{"invoice_number": number, "state": string(st.State)})
	}
	ev := payment.WebhookEvent{
		EventID:       fmt.Sprintf("reconcile:%s:%s", number, st.State),
		InvoiceNumber: number,
		ProviderRef:   st.ProviderRef,
		State:         st.State,
		Amount:        st.Amount,
		Currency:      st.Currency,
		Raw:           raw,
	}
	if err := s.ingest(ctx, pay, provider.Name(), ev); err != nil {
		return "", err
	}
	return string(st.State), nil
}
