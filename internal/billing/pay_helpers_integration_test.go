//go:build integration

package billing_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/billing"
	"github.com/kangsolder001/saka/internal/billing/payment"
	"github.com/kangsolder001/saka/internal/billing/payment/fake"
	"github.com/kangsolder001/saka/internal/entitlement"
	"github.com/kangsolder001/saka/internal/mail"
	"github.com/kangsolder001/saka/internal/org"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/outbox"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
	"github.com/kangsolder001/saka/internal/user"
	emailtemplates "github.com/kangsolder001/saka/templates/email"
)

const day = 24 * time.Hour

// payEnv is an env whose billing service has payments enabled: a fake gateway,
// the real contact lookup of the organization module and the real outbox mail
// service (so the email templates are rendered with the data billing gives them).
type payEnv struct {
	*env
	gateway *fake.Provider
	mail    *mail.Service
	org     *org.Service
	usage   *stubUsage
}

// stubUsage reports the usage a test sets, whatever the organization.
type stubUsage struct {
	byLimit map[string]entitlement.LimitUsage
}

func (u *stubUsage) Usage(context.Context, db.DBTX, uuid.UUID) (map[string]entitlement.LimitUsage, error) {
	return u.byLimit, nil
}

var testSettings = billing.Settings{
	Provider: fake.Name, InvoiceDue: 7 * day, RenewalLead: 3 * day, Grace: 3 * day, ReconcileMinAge: time.Hour,
}

func newPayEnv(t *testing.T) *payEnv {
	t.Helper()
	e := newEnv(t)
	gateway := fake.New()
	registry, err := payment.NewRegistry(gateway)
	require.NoError(t, err)
	organizations := org.NewService(org.Dependencies{Clock: e.clock, Tx: e.db, Users: user.NewService(e.clock)})
	renderer, err := mail.NewRenderer(emailtemplates.FS, mail.AppInfo{Name: "Saka"})
	require.NoError(t, err)
	mailer := mail.NewService(renderer, outbox.NewStore(e.db.Pool(), e.clock))
	usage := &stubUsage{byLimit: map[string]entitlement.LimitUsage{}}
	e.svc.EnablePayments(billing.PaymentDependencies{
		Tx: e.db, Mail: mailer, Contacts: organizations, Providers: registry, Usage: usage, Settings: testSettings, Log: logger.Nop(),
	})
	return &payEnv{env: e, gateway: gateway, mail: mailer, org: organizations, usage: usage}
}

// newFreeOrg is an organization on the Free plan.
func (e *payEnv) newFreeOrg() uuid.UUID {
	e.t.Helper()
	orgID := e.newOrg()
	e.inTenant(orgID, func(ctx context.Context, tx pgx.Tx) {
		require.NoError(e.t, e.svc.StartFree(ctx, tx, orgID))
	})
	return orgID
}

func (e *payEnv) ownerEmail(orgID uuid.UUID) string {
	e.t.Helper()
	var email string
	require.NoError(e.t, e.admin.QueryRow(context.Background(),
		"SELECT u.email FROM organizations o JOIN users u ON u.id = o.owner_id WHERE o.id = $1", orgID).Scan(&email))
	return email
}

// ctxAs is a request context inside the organization with the given role,
// as the tenant middleware would build it.
func ctxAs(orgID uuid.UUID, role org.Role) context.Context {
	return org.WithRole(reqctx.WithOrgID(context.Background(), orgID), role)
}

func ownerCtx(orgID uuid.UUID) context.Context { return ctxAs(orgID, org.RoleOwner) }

// subRow is a subscription as stored, through the owner connection.
type subRow struct {
	ID                 uuid.UUID
	Plan               string
	Status             billing.Status
	TrialEndsAt        *time.Time
	CurrentPeriodStart time.Time
	CurrentPeriodEnd   *time.Time
	CancelAtPeriodEnd  bool
	PendingPlan        *string
	GraceUntil         *time.Time
}

func (e *payEnv) sub(orgID uuid.UUID) subRow {
	e.t.Helper()
	var r subRow
	require.NoError(e.t, e.admin.QueryRow(context.Background(), `
		SELECT s.id, p.code, s.status, s.trial_ends_at, s.current_period_start, s.current_period_end,
		       s.cancel_at_period_end, pp.code, s.grace_until
		FROM subscriptions s JOIN plans p ON p.id = s.plan_id LEFT JOIN plans pp ON pp.id = s.pending_plan_id
		WHERE s.organization_id = $1`, orgID).Scan(&r.ID, &r.Plan, &r.Status, &r.TrialEndsAt, &r.CurrentPeriodStart,
		&r.CurrentPeriodEnd, &r.CancelAtPeriodEnd, &r.PendingPlan, &r.GraceUntil))
	return r
}

// setSub overwrites the subscription state, through the owner connection.
type subSpec struct {
	Plan              string
	Status            billing.Status
	TrialEndsAt       *time.Time
	PeriodStart       time.Time
	PeriodEnd         *time.Time
	CancelAtPeriodEnd bool
	PendingPlan       *string
	GraceUntil        *time.Time
}

func (e *payEnv) setSub(orgID uuid.UUID, s subSpec) {
	e.t.Helper()
	_, err := e.admin.Exec(context.Background(), `
		UPDATE subscriptions SET plan_id = (SELECT id FROM plans WHERE code = $2), status = $3, trial_ends_at = $4,
		       current_period_start = $5, current_period_end = $6, cancel_at_period_end = $7,
		       pending_plan_id = (SELECT id FROM plans WHERE code = $8), grace_until = $9
		WHERE organization_id = $1`,
		orgID, s.Plan, string(s.Status), s.TrialEndsAt, s.PeriodStart, s.PeriodEnd, s.CancelAtPeriodEnd, s.PendingPlan, s.GraceUntil)
	require.NoError(e.t, err)
}

// activePro puts the organization on Pro, active, with a period of 30 days that
// ends periodEndsIn from the epoch.
func (e *payEnv) activePro(orgID uuid.UUID, periodEndsIn time.Duration) {
	e.t.Helper()
	end := epoch.Add(periodEndsIn)
	e.setSub(orgID, subSpec{Plan: "pro", Status: billing.StatusActive, PeriodStart: end.AddDate(0, -1, 0), PeriodEnd: &end})
}

// invRow is an invoice as stored.
type invRow struct {
	ID          uuid.UUID
	Number      string
	Kind        string
	Plan        string
	Amount      int64
	Currency    string
	Status      string
	PeriodStart time.Time
	PeriodEnd   time.Time
	DueAt       time.Time
	PaidAt      *time.Time
	Provider    string
	ProviderRef *string
	PaymentURL  *string
	Reminded3d  *time.Time
	Reminded1d  *time.Time
	CreatedAt   time.Time
}

const invoiceColumns = `i.id, i.number, i.kind, p.code, i.amount, i.currency, i.status, i.period_start, i.period_end, i.due_at,
	i.paid_at, i.provider, i.provider_ref, i.payment_url, i.reminder_3d_sent_at, i.reminder_1d_sent_at, i.created_at`

func scanInvoice(row pgx.Row) (invRow, error) {
	var r invRow
	err := row.Scan(&r.ID, &r.Number, &r.Kind, &r.Plan, &r.Amount, &r.Currency, &r.Status, &r.PeriodStart, &r.PeriodEnd, &r.DueAt,
		&r.PaidAt, &r.Provider, &r.ProviderRef, &r.PaymentURL, &r.Reminded3d, &r.Reminded1d, &r.CreatedAt)
	return r, err
}

// invoices lists the organization's invoices, oldest first.
func (e *payEnv) invoices(orgID uuid.UUID) []invRow {
	e.t.Helper()
	rows, err := e.admin.Query(context.Background(),
		"SELECT "+invoiceColumns+" FROM invoices i JOIN plans p ON p.id = i.plan_id WHERE i.organization_id = $1 ORDER BY i.created_at, i.number", orgID)
	require.NoError(e.t, err)
	defer rows.Close()
	var out []invRow
	for rows.Next() {
		r, err := scanInvoice(rows)
		require.NoError(e.t, err)
		out = append(out, r)
	}
	require.NoError(e.t, rows.Err())
	return out
}

func (e *payEnv) invoiceByNumber(number string) invRow {
	e.t.Helper()
	r, err := scanInvoice(e.admin.QueryRow(context.Background(),
		"SELECT "+invoiceColumns+" FROM invoices i JOIN plans p ON p.id = i.plan_id WHERE i.number = $1", number))
	require.NoError(e.t, err)
	return r
}

// queuedEmail is an email job in the outbox.
type queuedEmail struct {
	Template string
	To       string
	Data     map[string]any
}

// emails lists the queued emails of a template (all templates when empty),
// oldest first.
func (e *payEnv) emails(template string) []queuedEmail {
	e.t.Helper()
	rows, err := e.admin.Query(context.Background(), "SELECT payload FROM outbox_jobs WHERE type = 'email.send' ORDER BY created_at, id")
	require.NoError(e.t, err)
	defer rows.Close()
	var out []queuedEmail
	for rows.Next() {
		var raw []byte
		require.NoError(e.t, rows.Scan(&raw))
		var q queuedEmail
		require.NoError(e.t, json.Unmarshal(raw, &q))
		if template == "" || q.Template == template {
			out = append(out, q)
		}
	}
	require.NoError(e.t, rows.Err())
	return out
}

func (e *payEnv) countRows(query string, args ...any) int {
	e.t.Helper()
	var n int
	require.NoError(e.t, e.admin.QueryRow(context.Background(), query, args...).Scan(&n))
	return n
}

// setTrialDays changes the trial length of a plan.
func (e *payEnv) setTrialDays(planCode string, days int) {
	e.t.Helper()
	_, err := e.admin.Exec(context.Background(), "UPDATE plans SET trial_days = $2 WHERE code = $1", planCode, days)
	require.NoError(e.t, err)
}

func at(t time.Time) *time.Time { return &t }
