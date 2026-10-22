package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/billing/billingdb"
	"github.com/kangsolder001/saka/internal/billing/payment"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

// CheckoutResult is what a checkout (or an upgrade) produced.
type CheckoutResult struct {
	Invoice Invoice
	// TrialStarted is true when this checkout put the organization on the
	// free trial of the plan (the subscription is trialing from now on).
	TrialStarted bool
	// Reused is true when an invoice for the same plan was still payable and
	// is returned as it was, instead of a second one being created.
	Reused bool
}

// Checkout starts buying a paid plan for the current organization: it creates
// the invoice and its payment link at the gateway and emails the invoice to
// the owner (spec §8.3).
//
// The client names the plan and nothing else. Amount, currency and the period
// come from the plans table; the subscription only changes through the state
// machine, and a paid plan only becomes active when the gateway's webhook says
// the invoice is paid, never because of this call or of a redirect.
//
//   - A payable invoice for the same plan is returned again instead of creating
//     a second one (calling checkout twice, or double-clicking, creates one).
//   - An organization still on Free that never trialed gets the trial of a plan
//     that has one: the subscription turns trialing at once, and the first
//     invoice, due when the trial ends, can be paid early.
//   - Everyone else gets an invoice that activates the plan once paid; an
//     older unpaid checkout for another plan is voided.
//   - An organization that already pays for this plan is told so (409).
//
// Owners only. The gateway is called inside the transaction, holding the
// subscription row lock, which makes concurrent checkouts of one organization
// run one after the other; if the gateway fails, nothing is stored.
func (s *Service) Checkout(ctx context.Context, planCode string) (CheckoutResult, error) {
	orgID, err := billingTenant(ctx)
	if err != nil {
		return CheckoutResult{}, err
	}
	pay, err := s.payments()
	if err != nil {
		return CheckoutResult{}, err
	}
	var res CheckoutResult
	err = pay.Tx.WithTx(ctx, func(tx pgx.Tx) error {
		plan, err := s.purchasablePlan(ctx, tx, planCode)
		if err != nil {
			return err
		}
		sub, err := lockSubscription(ctx, tx, orgID)
		if err != nil {
			return err
		}
		res, err = s.startPurchase(ctx, tx, pay, sub, plan)
		return err
	})
	return res, err
}

// purchasablePlan finds the plan a client asked for by code: it must exist, be
// on sale, and cost something (Free is never bought).
func (s *Service) purchasablePlan(ctx context.Context, q db.DBTX, code string) (billingdb.Plan, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return billingdb.Plan{}, apperr.Validation(map[string]string{"plan": "is required"})
	}
	plan, err := queries(q).GetPlanByCode(ctx, code)
	if errors.Is(err, pgx.ErrNoRows) {
		return billingdb.Plan{}, apperr.Validation(map[string]string{"plan": "is not a known plan"})
	}
	if err != nil {
		return billingdb.Plan{}, fmt.Errorf("billing: get plan: %w", err)
	}
	if !plan.IsActive || plan.PriceAmount <= 0 {
		return billingdb.Plan{}, apperr.Validation(map[string]string{"plan": "cannot be purchased"})
	}
	return plan, nil
}

func lockSubscription(ctx context.Context, q db.DBTX, orgID uuid.UUID) (billingdb.Subscription, error) {
	sub, err := queries(q).LockSubscriptionByOrganization(ctx, orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return billingdb.Subscription{}, apperr.NotFound()
	}
	if err != nil {
		return billingdb.Subscription{}, fmt.Errorf("billing: lock subscription: %w", err)
	}
	return sub, nil
}

// startPurchase is the shared body of checkout and of an upgrade: sub is the
// organization's locked subscription row, plan the plan to buy.
func (s *Service) startPurchase(ctx context.Context, tx pgx.Tx, pay *payments, sub billingdb.Subscription, plan billingdb.Plan) (CheckoutResult, error) {
	qs := queries(tx)
	now := s.clk.Now()
	status := Status(sub.Status)

	live, err := qs.LockLiveInvoicesForSubscription(ctx, sub.ID)
	if err != nil {
		return CheckoutResult{}, fmt.Errorf("billing: lock open invoices: %w", err)
	}
	for _, inv := range live {
		// An open invoice for this plan that can still be paid is the answer. A
		// renewal only counts for a subscription that is renewing.
		renewing := status == StatusActive || status == StatusPastDue
		if inv.PlanID == plan.ID && now.Before(pay.linkExpiry(InvoiceKind(inv.Kind), inv.DueAt)) &&
			(InvoiceKind(inv.Kind) == InvoiceKindCheckout || renewing) {
			return CheckoutResult{Invoice: invoiceFrom(inv, plan.Code, plan.Name), Reused: true}, nil
		}
	}
	if sub.PlanID == plan.ID && status == StatusActive {
		return CheckoutResult{}, apperr.Conflict("Organisasi sudah berlangganan paket ini.").WithDetails(map[string]any{"reason": "already_subscribed"})
	}
	// Whatever else is still waiting for a payment of a checkout is replaced by this one.
	if _, err := qs.VoidLiveInvoices(ctx, billingdb.VoidLiveInvoicesParams{
		SubscriptionID: sub.ID, Kind: ptr(string(InvoiceKindCheckout)), Now: now,
	}); err != nil {
		return CheckoutResult{}, fmt.Errorf("billing: void older checkouts: %w", err)
	}

	dueAt := now.Add(pay.Settings.InvoiceDue)
	trialStarted := false
	if plan.TrialDays > 0 {
		res, err := s.transition(ctx, tx, sub, Event{Kind: EventTrialStarted, Trial: &Trial{PlanID: plan.ID, Days: int(plan.TrialDays)}})
		switch {
		case err == nil:
			trialStarted = true
			dueAt = *res.Snapshot.TrialEndsAt
		case errors.Is(err, ErrNotEligible), errors.Is(err, ErrIllegalTransition):
			// Not the first time (or not a Free organization): no trial, just an invoice.
		default:
			return CheckoutResult{}, fmt.Errorf("billing: start trial: %w", err)
		}
	}

	periodEnd, err := AddInterval(now, plan.Interval)
	if err != nil {
		return CheckoutResult{}, fmt.Errorf("billing: plan %q: %w", plan.Code, err)
	}
	inv, err := s.issueInvoice(ctx, tx, pay, issue{
		OrganizationID: sub.OrganizationID, SubscriptionID: sub.ID, Kind: InvoiceKindCheckout, Plan: plan,
		PeriodStart: now, PeriodEnd: periodEnd, DueAt: dueAt,
	})
	if err != nil {
		return CheckoutResult{}, err
	}
	return CheckoutResult{Invoice: invoiceFrom(inv, plan.Code, plan.Name), TrialStarted: trialStarted}, nil
}

// issue describes an invoice to create.
type issue struct {
	OrganizationID, SubscriptionID uuid.UUID
	Kind                           InvoiceKind
	Plan                           billingdb.Plan
	PeriodStart, PeriodEnd, DueAt  time.Time
}

// issueInvoice numbers an invoice, creates its payment at the gateway, stores
// it open and emails it to the organization's owner, all in the caller's
// transaction (so a failure at any step leaves nothing behind but a gap in the
// numbering). The amount and currency are the plan's, copied: later price
// changes never touch an invoice that was issued.
func (s *Service) issueInvoice(ctx context.Context, tx pgx.Tx, pay *payments, in issue) (billingdb.Invoice, error) {
	provider, err := pay.provider()
	if err != nil {
		return billingdb.Invoice{}, err
	}
	contact, ok, err := pay.contactOf(ctx, tx, in.OrganizationID)
	if err != nil {
		return billingdb.Invoice{}, err
	}
	if !ok {
		return billingdb.Invoice{}, apperr.NotFound()
	}
	number, err := s.NextInvoiceNumber(ctx, tx)
	if err != nil {
		return billingdb.Invoice{}, err
	}
	now := s.clk.Now()

	session, err := provider.CreatePayment(ctx, payment.PaymentRequest{
		InvoiceNumber: number, Amount: in.Plan.PriceAmount, Currency: in.Plan.Currency,
		Description: fmt.Sprintf("Paket %s - %s", in.Plan.Name, intervalLabel(in.Plan.Interval)),
		Customer:    payment.Customer{Name: contact.OwnerName, Email: contact.OwnerEmail},
		ExpiresAt:   pay.linkExpiry(in.Kind, in.DueAt),
	})
	if err != nil {
		var gw *payment.GatewayError
		if errors.As(err, &gw) {
			zl := pay.Log.FromContext(reqctx.WithOrgID(ctx, in.OrganizationID))
			zl.Error().Str("provider", provider.Name()).Str("invoice_number", number).Err(err).Msg("payment gateway refused to create a payment")
			return billingdb.Invoice{}, apperr.PaymentProviderError(err)
		}
		return billingdb.Invoice{}, fmt.Errorf("billing: create payment: %w", err)
	}

	sent3d, sent1d := reminderMarks(now, in.DueAt)
	row, err := queries(tx).InsertInvoice(ctx, billingdb.InsertInvoiceParams{
		OrganizationID: in.OrganizationID, SubscriptionID: in.SubscriptionID, Number: number, Kind: string(in.Kind),
		PlanID: in.Plan.ID, Amount: in.Plan.PriceAmount, Currency: in.Plan.Currency, Status: string(InvoiceOpen),
		PeriodStart: in.PeriodStart, PeriodEnd: in.PeriodEnd, DueAt: in.DueAt, Provider: provider.Name(),
		ProviderRef: &session.Ref, PaymentUrl: &session.URL, Reminder3dSentAt: sent3d, Reminder1dSentAt: sent1d, Now: now,
	})
	if err != nil {
		return billingdb.Invoice{}, fmt.Errorf("billing: insert invoice: %w", err)
	}

	data := invoiceEmailData(contact, row, in.Plan)
	data["payment_url"] = session.URL
	if err := pay.sendEmail(ctx, tx, contact.OwnerEmail, "invoice_created", data); err != nil {
		return billingdb.Invoice{}, err
	}
	// Renewals are issued by a job, which has no tenant on the context: add it for the log line.
	zl := pay.Log.FromContext(reqctx.WithOrgID(ctx, in.OrganizationID))
	zl.Info().Str("invoice_number", number).Str("kind", string(in.Kind)).Str("plan", in.Plan.Code).Int64("amount", row.Amount).Msg("invoice issued")
	return row, nil
}

func intervalLabel(interval string) string {
	if interval == "year" {
		return "1 tahun"
	}
	return "1 bulan"
}

func ptr[T any](v T) *T { return &v }
