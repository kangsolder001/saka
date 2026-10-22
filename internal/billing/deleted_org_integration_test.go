//go:build integration

package billing_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/billing"
)

// endSubscription runs OrganizationDeleted in a tenant transaction, as the
// organization module does when it deletes the organization.
func (e *payEnv) endSubscription(orgID uuid.UUID) error {
	e.t.Helper()
	ctx := ownerCtx(orgID)
	return e.db.WithTx(ctx, func(tx pgx.Tx) error { return e.svc.OrganizationDeleted(ctx, tx, orgID) })
}

func TestOrganizationDeleted_CancelsEveryKindOfSubscription(t *testing.T) {
	end := epoch.AddDate(0, 0, 10)
	until := epoch.AddDate(0, 0, 2)
	setups := map[string]subSpec{
		"trialing": {Plan: "pro", Status: billing.StatusTrialing, TrialEndsAt: &end, PeriodStart: epoch, PeriodEnd: &end, CancelAtPeriodEnd: true},
		"active":   {Plan: "business", Status: billing.StatusActive, PeriodStart: epoch.AddDate(0, -1, 0), PeriodEnd: &end, PendingPlan: str("pro")},
		"past due": {Plan: "pro", Status: billing.StatusPastDue, PeriodStart: epoch.AddDate(0, -1, 0), PeriodEnd: &end, GraceUntil: &until},
		"expired":  {Plan: "pro", Status: billing.StatusExpired, PeriodStart: epoch.AddDate(0, -2, 0), PeriodEnd: &end},
		"free":     {Plan: "free", Status: billing.StatusActive, PeriodStart: epoch},
	}
	for name, spec := range setups {
		t.Run(name, func(t *testing.T) {
			e := newPayEnv(t)
			orgID := e.newFreeOrg()
			e.setSub(orgID, spec)

			require.NoError(t, e.endSubscription(orgID))

			sub := e.sub(orgID)
			assert.Equal(t, billing.StatusCanceled, sub.Status)
			assert.Nil(t, sub.PendingPlan)
			assert.Nil(t, sub.GraceUntil)
			assert.False(t, sub.CancelAtPeriodEnd)
			assert.Equal(t, spec.Plan, sub.Plan, "the record of what was subscribed stays")
		})
	}
}

func TestOrganizationDeleted_VoidsOpenInvoicesAndKeepsPaidOnes(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activePro(orgID, 2*day)
	e.insertRenewal(orgID, "INV-2026-000050", "pro", epoch.Add(2*day))
	checkout := e.buy(orgID, "business")
	_, err := e.admin.Exec(context.Background(), `
		INSERT INTO invoices (organization_id, subscription_id, number, kind, plan_id, amount, currency, status, period_start, period_end, due_at, paid_at, provider)
		SELECT $1, s.id, 'INV-2026-000777', 'checkout', s.plan_id, 99000, 'IDR', 'paid', now(), now() + interval '1 month', now(), now(), 'fake'
		FROM subscriptions s WHERE s.organization_id = $1`, orgID)
	require.NoError(t, err)

	require.NoError(t, e.endSubscription(orgID))

	assert.Equal(t, "void", e.invoiceByNumber("INV-2026-000050").Status)
	assert.Equal(t, "void", e.invoiceByNumber(checkout.Number).Status)
	assert.Equal(t, "paid", e.invoiceByNumber("INV-2026-000777").Status, "history is not rewritten")
}

func TestOrganizationDeleted_IsSafeToRepeatAndLeavesOtherOrganizationsAlone(t *testing.T) {
	e := newPayEnv(t)
	gone, other := e.newFreeOrg(), e.newFreeOrg()
	e.buy(gone, "pro")
	e.buy(other, "pro")

	require.NoError(t, e.endSubscription(gone))
	require.NoError(t, e.endSubscription(gone))

	assert.Equal(t, billing.StatusCanceled, e.sub(gone).Status)
	assert.Equal(t, billing.StatusTrialing, e.sub(other).Status)
	assert.Equal(t, "open", e.invoices(other)[0].Status)
}

func TestOrganizationDeleted_AnOrganizationWithoutASubscriptionIsFine(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newOrg() // no subscription row

	assert.NoError(t, e.endSubscription(orgID))
}

func TestOrganizationDeleted_APaymentThatArrivesLaterIsFlaggedNotActivated(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	inv := e.buy(orgID, "pro")
	require.NoError(t, e.endSubscription(orgID))

	require.NoError(t, e.deliver(paidEvent("evt-1", inv.Number, 99000)), "answered with success; a retry would change nothing")

	assert.Equal(t, billing.StatusCanceled, e.sub(orgID).Status, "a deleted organization does not come back by paying a link")
	assert.Equal(t, "void", e.invoiceByNumber(inv.Number).Status)
	requireFlagged(t, e.events()[0], "void")
}
