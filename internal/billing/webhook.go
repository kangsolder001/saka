package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/billing/billingdb"
	"github.com/kangsolder001/saka/internal/billing/payment"
	"github.com/kangsolder001/saka/internal/platform/apperr"
)

// HandleWebhook handles a payment notification from a gateway (spec §8.5).
//
//  1. The gateway's own code verifies the signature before anything else. A
//     notification that does not verify is UNAUTHENTICATED (401) and nothing is
//     stored; one that cannot be read is a VALIDATION_ERROR (400).
//  2. The verified event is stored in payment_events. A delivery the gateway
//     repeats (they retry whenever they get no 2xx) matches the same
//     (provider, event_id) and is stored once.
//
// A webhook belongs to no tenant until it is matched to an invoice, so it runs
// in system transactions with explicit filters. The status of a payment is
// only ever taken from here or from GetPaymentStatus, never from the browser
// of the customer.
func (s *Service) HandleWebhook(ctx context.Context, providerName string, r *http.Request) error {
	pay, err := s.payments()
	if err != nil {
		return err
	}
	provider, ok := pay.Providers[providerName]
	if !ok {
		return apperr.NotFound()
	}
	ev, err := provider.VerifyWebhook(r)
	switch {
	case errors.Is(err, payment.ErrInvalidSignature):
		zl := pay.Log.FromContext(ctx)
		zl.Warn().Str("provider", providerName).Msg("webhook rejected: invalid signature")
		return apperr.Unauthenticated()
	case errors.Is(err, payment.ErrMalformedEvent):
		return apperr.Validation(map[string]string{"body": "is not a valid notification"})
	case err != nil:
		return fmt.Errorf("billing: verify %s webhook: %w", providerName, err)
	}
	return s.ingest(ctx, pay, provider.Name(), ev)
}

// ingest stores a verified event and processes it. Both steps are idempotent
// and safe to run twice or at the same time:
//
//  1. The event is stored in a transaction of its own that commits whatever
//     happens next, so a failure while processing still leaves the event (and
//     the reason) on record for the gateway's retry and for a person.
//  2. In a second transaction the event row is locked (a duplicate delivery or
//     a retry waits for the first to finish), and if it was already processed
//     nothing is done. Otherwise the payment is applied: invoice first, then
//     subscription, then the email job, all or nothing. Success sets
//     processed_at; failure rolls everything back, stores the error, and is
//     returned (a 500) so the gateway sends the event again. Something a
//     person must look at (a payment for a void invoice, the wrong amount) is
//     not a failure: it is processed and its note stored beside processed_at.
func (s *Service) ingest(ctx context.Context, pay *payments, provider string, ev payment.WebhookEvent) error {
	payload := ev.Raw
	if !json.Valid(payload) {
		// The contract says Raw is JSON; never lose an event over a provider's slip.
		payload, _ = json.Marshal(map[string]string{"raw": string(ev.Raw)})
	}
	err := pay.Tx.WithSystemTx(ctx, func(tx pgx.Tx) error {
		_, err := queries(tx).InsertPaymentEvent(ctx, billingdb.InsertPaymentEventParams{
			Provider: provider, EventID: ev.EventID, Payload: payload, Now: s.clk.Now(),
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) { // no row: a delivery we already stored
			return fmt.Errorf("billing: store payment event: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	var rowID uuid.UUID
	var failure error
	err = pay.Tx.WithSystemTx(ctx, func(tx pgx.Tx) error {
		row, err := queries(tx).LockPaymentEvent(ctx, billingdb.LockPaymentEventParams{Provider: provider, EventID: ev.EventID})
		if err != nil {
			return fmt.Errorf("billing: lock payment event: %w", err)
		}
		rowID = row.ID
		if row.ProcessedAt != nil {
			return nil // delivered twice: the first delivery did the work
		}
		note, err := s.applyPayment(ctx, tx, pay, provider, ev)
		if err != nil {
			failure = err
			return err
		}
		var noteArg *string
		if note != "" {
			noteArg = &note
		}
		if err := queries(tx).MarkPaymentEventProcessed(ctx, billingdb.MarkPaymentEventProcessedParams{ID: row.ID, Now: s.clk.Now(), Note: noteArg}); err != nil {
			return fmt.Errorf("billing: mark payment event processed: %w", err)
		}
		if note != "" {
			zl := pay.Log.FromContext(ctx)
			zl.Error().Str("provider", provider).Str("event_id", ev.EventID).Str("invoice_number", ev.InvoiceNumber).
				Str("note", note).Msg("payment event needs manual attention")
		}
		return nil
	})
	if err == nil {
		return nil
	}
	if failure != nil && rowID != uuid.Nil {
		s.recordEventFailure(ctx, pay, rowID, failure)
	}
	return fmt.Errorf("billing: process %s event %s: %w", provider, ev.EventID, err)
}

// recordEventFailure stores why an event could not be processed, in a
// transaction of its own (the processing one was rolled back). Failing to
// record it is only logged: the original error is what the caller returns.
func (s *Service) recordEventFailure(ctx context.Context, pay *payments, id uuid.UUID, failure error) {
	msg := failure.Error()
	if len(msg) > maxEventError {
		msg = msg[:maxEventError]
	}
	detached := context.WithoutCancel(ctx) // the request may already be over; the record must still be made
	err := pay.Tx.WithSystemTx(detached, func(tx pgx.Tx) error {
		return queries(tx).SetPaymentEventError(detached, billingdb.SetPaymentEventErrorParams{ID: id, Error: &msg})
	})
	if err != nil {
		zl := pay.Log.FromContext(ctx)
		zl.Error().Err(err).Msg("could not record the failure of a payment event")
	}
}

// maxEventError caps the failure text stored on an event.
const maxEventError = 1000

// applyPayment applies one verified event to the invoice it names and to that
// invoice's subscription, in the caller's system transaction. It returns a
// note when the event was understood but needs a person (nothing is
// activated), and an error only when something broke (the caller rolls back
// and the gateway retries).
//
// Lock order is always subscription, then invoice, like every other writer
// (checkout, jobs), so a webhook and a job on the same organization cannot
// deadlock. Late and repeated events never move an invoice backwards: a paid
// invoice stays paid whatever "expired" arrives afterwards.
func (s *Service) applyPayment(ctx context.Context, tx pgx.Tx, pay *payments, provider string, ev payment.WebhookEvent) (string, error) {
	qs := queries(tx)
	found, err := qs.GetInvoiceByNumber(ctx, ev.InvoiceNumber)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Sprintf("no invoice has the number %q", ev.InvoiceNumber), nil
	}
	if err != nil {
		return "", fmt.Errorf("billing: find invoice: %w", err)
	}
	sub, err := qs.LockSubscriptionByID(ctx, found.SubscriptionID)
	if err != nil {
		return "", fmt.Errorf("billing: lock subscription: %w", err)
	}
	inv, err := qs.LockInvoice(ctx, found.ID)
	if err != nil {
		return "", fmt.Errorf("billing: lock invoice: %w", err)
	}
	if inv.Provider != provider {
		return fmt.Sprintf("invoice %s belongs to provider %q, not %q", inv.Number, inv.Provider, provider), nil
	}
	plan, err := qs.GetPlanByID(ctx, inv.PlanID)
	if err != nil {
		return "", fmt.Errorf("billing: get plan of invoice: %w", err)
	}
	now := s.clk.Now()

	switch ev.State {
	case payment.StatePaid:
		return s.applyPaid(ctx, tx, pay, sub, inv, plan, ev)
	case payment.StateExpired:
		if _, err := qs.CloseInvoice(ctx, billingdb.CloseInvoiceParams{ID: inv.ID, Status: string(InvoiceExpired), Now: now}); err != nil {
			return "", fmt.Errorf("billing: expire invoice: %w", err)
		}
	case payment.StateCanceled:
		if _, err := qs.CloseInvoice(ctx, billingdb.CloseInvoiceParams{ID: inv.ID, Status: string(InvoiceVoid), Now: now}); err != nil {
			return "", fmt.Errorf("billing: void invoice: %w", err)
		}
	case payment.StateRefunded:
		return fmt.Sprintf("the gateway reports a refund or chargeback on invoice %s (status %s); handle it by hand", inv.Number, inv.Status), nil
	case payment.StatePending, payment.StateFailed:
		// An attempt that is under way or was refused: the invoice stays open for
		// another try; nothing to do.
	default:
		return fmt.Sprintf("unknown payment state %q on invoice %s", ev.State, inv.Number), nil
	}
	return "", nil
}

// applyPaid handles a "paid" event for a locked invoice and its subscription.
func (s *Service) applyPaid(ctx context.Context, tx pgx.Tx, pay *payments, sub billingdb.Subscription, inv billingdb.Invoice, plan billingdb.Plan, ev payment.WebhookEvent) (string, error) {
	qs := queries(tx)
	now := s.clk.Now()

	switch InvoiceStatus(inv.Status) {
	case InvoicePaid:
		return "", nil // the second notification of one payment (capture, then settlement)
	case InvoiceVoid, InvoiceExpired:
		// Money arrived for an invoice we had closed: never activate silently.
		return fmt.Sprintf("payment received for invoice %s, which is %s; refund it or reopen the subscription by hand", inv.Number, inv.Status), nil
	}
	// The amount and currency come from the invoice we issued, never from the
	// notification: a payment for another amount is not this invoice's payment.
	if ev.Amount != inv.Amount || !strings.EqualFold(ev.Currency, inv.Currency) {
		return fmt.Sprintf("amount mismatch on invoice %s: expected %d %s, the gateway reports %d %s",
			inv.Number, inv.Amount, inv.Currency, ev.Amount, ev.Currency), nil
	}

	res, err := s.transition(ctx, tx, sub, Event{Kind: EventInvoicePaid, Invoice: ptr(paidInvoiceOf(inv, plan))})
	stale := ""
	switch {
	case err == nil:
	case errors.Is(err, ErrStaleInvoice), errors.Is(err, ErrIllegalTransition):
		// The money is real, but the subscription can no longer use this invoice
		// (it expired or was canceled meanwhile). Record the payment, change
		// nothing else, and ask a person to decide.
		stale = fmt.Sprintf("invoice %s was paid but does not fit the %s subscription (%v); settle it by hand", inv.Number, sub.Status, err)
	default:
		return "", fmt.Errorf("billing: apply payment to subscription: %w", err)
	}

	periodStart, periodEnd := inv.PeriodStart, inv.PeriodEnd
	if err == nil && res.Outcome == OutcomeActivated {
		// A checkout is paid for the period that starts now (spec §8.4).
		periodStart, periodEnd = res.Snapshot.CurrentPeriodStart, *res.Snapshot.CurrentPeriodEnd
	}
	var providerRef *string
	if ev.ProviderRef != "" {
		providerRef = &ev.ProviderRef
	}
	if err := qs.MarkInvoicePaid(ctx, billingdb.MarkInvoicePaidParams{
		ID: inv.ID, PaidAt: now, PeriodStart: periodStart, PeriodEnd: periodEnd, ProviderRef: providerRef,
	}); err != nil {
		return "", fmt.Errorf("billing: mark invoice paid: %w", err)
	}
	if stale != "" {
		return stale, nil
	}
	if res.Outcome == OutcomeActivated {
		// A new period began: what was still waiting to be paid for the old one is moot.
		if _, err := qs.VoidLiveInvoices(ctx, billingdb.VoidLiveInvoicesParams{SubscriptionID: sub.ID, Now: now}); err != nil {
			return "", fmt.Errorf("billing: void other open invoices: %w", err)
		}
	}

	contact, ok, err := pay.contactOf(ctx, tx, inv.OrganizationID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", nil // nobody left to tell; the payment itself is recorded
	}
	data := invoiceEmailData(contact, inv, plan)
	data["period_end"] = FormatDate(periodEnd)
	if res.Changed && res.Snapshot.CurrentPeriodEnd != nil {
		data["period_end"] = FormatDate(*res.Snapshot.CurrentPeriodEnd)
	}
	if err := pay.sendEmail(ctx, tx, contact.OwnerEmail, "payment_succeeded", data); err != nil {
		return "", err
	}
	zl := pay.Log.FromContext(ctx)
	zl.Info().Str("invoice_number", inv.Number).Str("outcome", string(res.Outcome)).Str("plan", plan.Code).Msg("invoice paid")
	return "", nil
}
