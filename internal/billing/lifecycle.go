package billing

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/kangsolder001/saka/internal/billing/billingdb"
	"github.com/kangsolder001/saka/internal/mail"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/db"
)

// This file connects the pure state machine to the database: it reads a
// subscription row into a Snapshot, asks Next what an event does, and writes
// the answer back. The rules live in statemachine.go; nothing here decides
// them.

func snapshotOf(r billingdb.Subscription) Snapshot {
	return Snapshot{
		Status: Status(r.Status), PlanID: r.PlanID, TrialEndsAt: r.TrialEndsAt,
		CurrentPeriodStart: r.CurrentPeriodStart, CurrentPeriodEnd: r.CurrentPeriodEnd,
		CancelAtPeriodEnd: r.CancelAtPeriodEnd, PendingPlanID: r.PendingPlanID, GraceUntil: r.GraceUntil,
	}
}

// saveSnapshot writes the state machine's fields of subscription id.
func (s *Service) saveSnapshot(ctx context.Context, q db.DBTX, id uuid.UUID, snap Snapshot) error {
	err := queries(q).UpdateSubscriptionState(ctx, billingdb.UpdateSubscriptionStateParams{
		ID: id, Status: string(snap.Status), PlanID: snap.PlanID, TrialEndsAt: snap.TrialEndsAt,
		CurrentPeriodStart: snap.CurrentPeriodStart, CurrentPeriodEnd: snap.CurrentPeriodEnd,
		CancelAtPeriodEnd: snap.CancelAtPeriodEnd, PendingPlanID: snap.PendingPlanID, GraceUntil: snap.GraceUntil,
		Now: s.clk.Now(),
	})
	if err != nil {
		return fmt.Errorf("billing: update subscription: %w", err)
	}
	return nil
}

// transition applies event to the locked subscription row sub, saves the
// result when something changed, and returns it. An event the rules refuse is
// returned as the state machine's own error, for the caller to interpret.
func (s *Service) transition(ctx context.Context, q db.DBTX, sub billingdb.Subscription, event Event) (Result, error) {
	res, err := Next(snapshotOf(sub), event, s.clk.Now())
	if err != nil {
		return Result{}, err
	}
	if res.Changed {
		if err := s.saveSnapshot(ctx, q, sub.ID, res.Snapshot); err != nil {
			return Result{}, err
		}
	}
	return res, nil
}

// paidInvoiceOf describes an invoice row for the state machine. The plan's
// billing interval lengthens the period of a checkout.
func paidInvoiceOf(inv billingdb.Invoice, plan billingdb.Plan) PaidInvoice {
	return PaidInvoice{
		Kind: InvoiceKind(inv.Kind), PlanID: inv.PlanID, Interval: plan.Interval,
		PeriodStart: inv.PeriodStart, PeriodEnd: inv.PeriodEnd,
	}
}

// contactOf finds whom to write to about an organization. A missing contact
// (the organization or its owner is gone) is not an error for billing: the
// work goes on and nobody is emailed.
func (p *payments) contactOf(ctx context.Context, q db.DBTX, orgID uuid.UUID) (Contact, bool, error) {
	c, err := p.Contacts.BillingContact(ctx, q, orgID)
	if apperr.IsCode(err, apperr.CodeNotFound) {
		return Contact{}, false, nil
	}
	if err != nil {
		return Contact{}, false, fmt.Errorf("billing: find the contact of the organization: %w", err)
	}
	return c, true, nil
}

// sendEmail queues a billing email in the caller's transaction.
func (p *payments) sendEmail(ctx context.Context, q db.DBTX, to, template string, data map[string]any) error {
	if err := p.Mail.Send(ctx, q, mail.Email{Template: template, To: to, Data: data}); err != nil {
		return fmt.Errorf("billing: queue %s email: %w", template, err)
	}
	return nil
}

// invoiceEmailData is what the invoice templates share.
func invoiceEmailData(c Contact, inv billingdb.Invoice, plan billingdb.Plan) map[string]any {
	return map[string]any{
		"name":              c.OwnerName,
		"organization_name": c.OrganizationName,
		"invoice_number":    inv.Number,
		"plan_name":         plan.Name,
		"amount":            FormatMoney(inv.Amount, inv.Currency),
		"due_date":          FormatDate(inv.DueAt),
	}
}

// reminderMarks returns the reminder timestamps a new invoice starts with.
// Reminders are sent at 3 and 1 days before the due date (billing.reminders);
// an invoice created inside such a window is itself the notice for it, so
// that reminder is marked as sent from the start and the customer is not told
// twice within minutes.
func reminderMarks(now, dueAt time.Time) (sent3d, sent1d *time.Time) {
	remaining := dueAt.Sub(now)
	if remaining <= 3*day {
		sent3d = &now
	}
	if remaining <= day {
		sent1d = &now
	}
	return sent3d, sent1d
}
