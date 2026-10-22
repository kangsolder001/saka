package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/billing/payment"
	"github.com/kangsolder001/saka/internal/billing/payment/fake"
	"github.com/kangsolder001/saka/internal/billing/payment/midtrans"
	"github.com/kangsolder001/saka/internal/mail"
	"github.com/kangsolder001/saka/internal/org"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/logger"
)

// Contact is whom billing writes to about an organization: its owner, the one
// who may manage billing (spec §7.2). The organization module defines it.
type Contact = org.BillingContact

// OrganizationContacts finds the contact of an organization (*org.Service
// implements it); billing declares it so it never reads organization or user
// tables itself.
type OrganizationContacts interface {
	// BillingContact returns NOT_FOUND when the organization or its owner no
	// longer exists. It also finds organizations that were deleted, because a
	// late payment for one still needs someone to be told.
	BillingContact(ctx context.Context, q db.DBTX, orgID uuid.UUID) (Contact, error)
}

// Database opens the transactions billing needs: pinned to the tenant on the
// context (requests) or across tenants (webhooks and jobs). *db.DB implements
// it.
type Database interface {
	WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error
	WithSystemTx(ctx context.Context, fn func(tx pgx.Tx) error) error
}

var _ Database = (*db.DB)(nil)

// Settings are the billing rules that come from configuration (spec §8.6).
type Settings struct {
	// Provider names the gateway new invoices are created with.
	Provider string
	// InvoiceDue is how long a checkout invoice stays payable.
	InvoiceDue time.Duration
	// RenewalLead is how long before a period ends its renewal invoice is issued.
	RenewalLead time.Duration
	// Grace is how long a subscription keeps access after its period ended unpaid.
	Grace time.Duration
	// ReconcileMinAge is how old an open invoice must be before reconcile asks the gateway.
	ReconcileMinAge time.Duration
	// HourlyInterval and DailyInterval are how often the worker runs the
	// hourly jobs (renewals, expire, apply_scheduled) and the daily ones
	// (reminders, reconcile).
	HourlyInterval, DailyInterval time.Duration
}

const day = 24 * time.Hour

// SettingsFrom reads the settings from the validated configuration.
func SettingsFrom(c config.BillingConfig) Settings {
	return Settings{
		Provider:        c.Provider,
		InvoiceDue:      time.Duration(c.InvoiceDueDays) * day,
		RenewalLead:     time.Duration(c.RenewalLeadDays) * day,
		Grace:           time.Duration(c.GraceDays) * day,
		ReconcileMinAge: c.ReconcileMinAge.Std(),
		HourlyInterval:  c.HourlyJobsInterval.Std(),
		DailyInterval:   c.DailyJobsInterval.Std(),
	}
}

// PaymentDependencies are what the payment side of billing needs on top of the
// read side every Service has. The composition roots (API and worker) provide
// them.
type PaymentDependencies struct {
	Tx        Database
	Mail      mail.Sender
	Contacts  OrganizationContacts
	Providers payment.Registry
	// Usage reports an organization's usage, to warn about a downgrade that
	// would leave it over the limits of the smaller plan.
	Usage    UsageReporter
	Settings Settings
	Log      *logger.Logger
}

// payments is the payment side of a Service, present only when configured.
type payments struct {
	PaymentDependencies
}

// EnablePayments turns on checkout, invoices, webhooks and the billing jobs.
// It is separate from NewService because the organization module needs the
// Service (to start Free subscriptions) before the contact lookup of billing,
// which is that same module, exists: the composition root closes the loop here,
// like it does for the other modules that need each other. Call it once,
// before the Service is used.
func (s *Service) EnablePayments(deps PaymentDependencies) {
	s.pay = &payments{PaymentDependencies: deps}
}

var errPaymentsNotConfigured = errors.New("billing: payments are not configured on this service")

// payments returns the payment side, or an error when EnablePayments was never
// called (a wiring bug).
func (s *Service) payments() (*payments, error) {
	if s.pay == nil {
		return nil, errPaymentsNotConfigured
	}
	return s.pay, nil
}

// provider returns the gateway new invoices use.
func (p *payments) provider() (payment.Provider, error) {
	pr, ok := p.Providers[p.Settings.Provider]
	if !ok {
		return nil, fmt.Errorf("billing: payment provider %q is not registered", p.Settings.Provider)
	}
	return pr, nil
}

// NewProviders builds the payment gateways the configuration enables. Webhooks
// of each are accepted; new invoices use cfg.Provider. clk is the clock the
// gateway uses to compute expiry.
func NewProviders(cfg config.BillingConfig, clk clock.Clock) (payment.Registry, error) {
	var providers []payment.Provider
	switch cfg.Provider {
	case config.BillingProviderMidtrans:
		m, err := midtrans.New(midtrans.Config{
			ServerKey:   cfg.Midtrans.ServerKey,
			SnapBaseURL: cfg.Midtrans.SnapBaseURL,
			APIBaseURL:  cfg.Midtrans.APIBaseURL,
			Timeout:     cfg.Midtrans.RequestTimeout.Std(),
		}, clk)
		if err != nil {
			return nil, err
		}
		providers = append(providers, m)
	case config.BillingProviderFake:
		providers = append(providers, fake.New())
	default:
		return nil, fmt.Errorf("billing: unknown payment provider %q", cfg.Provider)
	}
	return payment.NewRegistry(providers...)
}
