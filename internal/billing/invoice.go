package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/billing/billingdb"
	"github.com/kangsolder001/saka/internal/org"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/pagination"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

// InvoiceStatus is the state of an invoice (spec §4.9).
type InvoiceStatus string

const (
	// InvoiceDraft is reserved: the checkout and the jobs create invoices
	// inside one transaction together with their payment, so none is ever
	// stored as a draft.
	InvoiceDraft   InvoiceStatus = "draft"
	InvoiceOpen    InvoiceStatus = "open"
	InvoicePaid    InvoiceStatus = "paid"
	InvoiceExpired InvoiceStatus = "expired"
	InvoiceVoid    InvoiceStatus = "void"
)

// Invoice is the bill of one period, with its plan named by code.
type Invoice struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	SubscriptionID uuid.UUID
	Number         string
	Kind           InvoiceKind
	PlanCode       string
	PlanName       string
	// Amount is in the smallest unit of Currency (IDR: rupiah).
	Amount      int64
	Currency    string
	Status      InvoiceStatus
	PeriodStart time.Time
	PeriodEnd   time.Time
	DueAt       time.Time
	PaidAt      *time.Time
	Provider    string
	// PaymentURL is where the invoice is paid; only set while it can be.
	PaymentURL *string
	CreatedAt  time.Time
}

// CursorOf is the keyset position of the invoice in a listing.
func (i Invoice) CursorOf() pagination.Cursor {
	return pagination.Cursor{CreatedAt: i.CreatedAt, ID: i.ID}
}

func invoiceFrom(r billingdb.Invoice, planCode, planName string) Invoice {
	inv := Invoice{
		ID: r.ID, OrganizationID: r.OrganizationID, SubscriptionID: r.SubscriptionID, Number: r.Number,
		Kind: InvoiceKind(r.Kind), PlanCode: planCode, PlanName: planName, Amount: r.Amount, Currency: r.Currency,
		Status: InvoiceStatus(r.Status), PeriodStart: r.PeriodStart, PeriodEnd: r.PeriodEnd, DueAt: r.DueAt,
		PaidAt: r.PaidAt, Provider: r.Provider, CreatedAt: r.CreatedAt,
	}
	// A link that can no longer be paid is not handed out.
	if inv.Status == InvoiceOpen {
		inv.PaymentURL = r.PaymentUrl
	}
	return inv
}

func invoiceFromGet(r billingdb.GetInvoiceRow) Invoice {
	return invoiceFrom(billingdb.Invoice{
		ID: r.ID, OrganizationID: r.OrganizationID, SubscriptionID: r.SubscriptionID, Number: r.Number, Kind: r.Kind,
		PlanID: r.PlanID, Amount: r.Amount, Currency: r.Currency, Status: r.Status, PeriodStart: r.PeriodStart,
		PeriodEnd: r.PeriodEnd, DueAt: r.DueAt, PaidAt: r.PaidAt, Provider: r.Provider, ProviderRef: r.ProviderRef,
		PaymentUrl: r.PaymentUrl, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}, r.PlanCode, r.PlanName)
}

func invoiceFromList(r billingdb.ListInvoicesRow) Invoice {
	return invoiceFrom(billingdb.Invoice{
		ID: r.ID, OrganizationID: r.OrganizationID, SubscriptionID: r.SubscriptionID, Number: r.Number, Kind: r.Kind,
		PlanID: r.PlanID, Amount: r.Amount, Currency: r.Currency, Status: r.Status, PeriodStart: r.PeriodStart,
		PeriodEnd: r.PeriodEnd, DueAt: r.DueAt, PaidAt: r.PaidAt, Provider: r.Provider, ProviderRef: r.ProviderRef,
		PaymentUrl: r.PaymentUrl, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}, r.PlanCode, r.PlanName)
}

// billingTenant checks that the caller may manage billing of the organization
// in ctx and returns that organization. It is how every billing method of the
// service asks "who, and may they": the role comes from the tenant middleware,
// never from input (spec §7.3).
func billingTenant(ctx context.Context) (uuid.UUID, error) {
	if err := org.Authorize(ctx, org.ActionManageBilling); err != nil {
		return uuid.Nil, err
	}
	orgID, ok := reqctx.OrgID(ctx)
	if !ok {
		return uuid.Nil, apperr.NotFound()
	}
	return orgID, nil
}

// ListInvoices returns the organization's invoices, newest first, as one
// extra row beyond p.Limit so the caller can build the next cursor with
// pagination.Paginate. Owners only (spec §7.2: billing).
func (s *Service) ListInvoices(ctx context.Context, p pagination.Params) ([]Invoice, error) {
	orgID, err := billingTenant(ctx)
	if err != nil {
		return nil, err
	}
	pay, err := s.payments()
	if err != nil {
		return nil, err
	}
	params := billingdb.ListInvoicesParams{OrganizationID: orgID, RowLimit: int32(p.FetchLimit())} //nolint:gosec // bounded by pagination.MaxLimit
	if p.Cursor != nil {
		params.BeforeCreatedAt, params.BeforeID = &p.Cursor.CreatedAt, &p.Cursor.ID
	}
	var out []Invoice
	err = pay.Tx.WithTx(ctx, func(tx pgx.Tx) error {
		rows, err := queries(tx).ListInvoices(ctx, params)
		if err != nil {
			return fmt.Errorf("billing: list invoices: %w", err)
		}
		out = make([]Invoice, len(rows))
		for i, r := range rows {
			out[i] = invoiceFromList(r)
		}
		return nil
	})
	return out, err
}

// GetInvoice returns one invoice of the organization, or NOT_FOUND when it does
// not exist or belongs to another organization. Owners only.
func (s *Service) GetInvoice(ctx context.Context, id uuid.UUID) (Invoice, error) {
	orgID, err := billingTenant(ctx)
	if err != nil {
		return Invoice{}, err
	}
	pay, err := s.payments()
	if err != nil {
		return Invoice{}, err
	}
	var out Invoice
	err = pay.Tx.WithTx(ctx, func(tx pgx.Tx) error {
		row, err := queries(tx).GetInvoice(ctx, billingdb.GetInvoiceParams{ID: id, OrganizationID: orgID})
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return fmt.Errorf("billing: get invoice: %w", err)
		}
		out = invoiceFromGet(row)
		return nil
	})
	return out, err
}

// linkExpiry is the instant the payment link of an invoice stops working: a
// checkout invoice at its due date, a renewal after the grace period that
// follows its due date (the subscription keeps its access until then, so the
// customer can still pay).
func (p *payments) linkExpiry(kind InvoiceKind, dueAt time.Time) time.Time {
	if kind == InvoiceKindRenewal {
		return dueAt.Add(p.Settings.Grace)
	}
	return dueAt
}
