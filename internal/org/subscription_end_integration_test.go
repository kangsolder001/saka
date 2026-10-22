//go:build integration

package org_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/org"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

// putOnTrialWithAnOpenInvoice gives the organization a trialing Pro
// subscription and an invoice waiting for payment, as a checkout leaves it.
func (e *env) putOnTrialWithAnOpenInvoice(orgID uuid.UUID) {
	e.t.Helper()
	ctx := context.Background()
	_, err := e.admin.Exec(ctx, `
		UPDATE subscriptions SET plan_id = (SELECT id FROM plans WHERE code = 'pro'), status = 'trialing',
		       trial_ends_at = now() + interval '14 days', current_period_end = now() + interval '14 days',
		       cancel_at_period_end = true, pending_plan_id = (SELECT id FROM plans WHERE code = 'business')
		WHERE organization_id = $1`, orgID)
	require.NoError(e.t, err)
	_, err = e.admin.Exec(ctx, `
		INSERT INTO invoices (organization_id, subscription_id, number, kind, plan_id, amount, currency, status, period_start, period_end, due_at, provider)
		SELECT $1, s.id, 'INV-2026-000001', 'checkout', s.plan_id, 99000, 'IDR', 'open', now(), now() + interval '1 month', now() + interval '14 days', 'fake'
		FROM subscriptions s WHERE s.organization_id = $1`, orgID)
	require.NoError(e.t, err)
}

type subscriptionEnd struct {
	status        string
	pendingPlan   *string
	cancelAtEnd   bool
	invoiceStatus string
}

func (e *env) subscriptionEnd(orgID uuid.UUID) subscriptionEnd {
	e.t.Helper()
	var r subscriptionEnd
	require.NoError(e.t, e.admin.QueryRow(context.Background(), `
		SELECT s.status, pp.code, s.cancel_at_period_end, COALESCE((SELECT status FROM invoices WHERE organization_id = $1), '')
		FROM subscriptions s LEFT JOIN plans pp ON pp.id = s.pending_plan_id WHERE s.organization_id = $1`, orgID).
		Scan(&r.status, &r.pendingPlan, &r.cancelAtEnd, &r.invoiceStatus))
	return r
}

func TestDelete_CancelsTheSubscriptionAndVoidsItsOpenInvoices(t *testing.T) {
	e := newEnv(t)
	ann := e.newUser("ann@example.com")
	acme, err := e.create(ann.ID, "Acme", "")
	require.NoError(t, err)
	e.putOnTrialWithAnOpenInvoice(acme.ID)
	ctx := org.WithRole(reqctx.WithOrgID(context.Background(), acme.ID), org.RoleOwner)

	require.NoError(t, e.orgs.Delete(ctx))

	assert.True(t, e.orgDeleted(acme.ID))
	got := e.subscriptionEnd(acme.ID)
	assert.Equal(t, "canceled", got.status)
	assert.Equal(t, "void", got.invoiceStatus)
	assert.Nil(t, got.pendingPlan, "no scheduled change survives")
	assert.False(t, got.cancelAtEnd)
}

func TestGuard_AccountDeletionCancelsTheSubscriptionOfTheOrganizationItTakesWithIt(t *testing.T) {
	e := newEnv(t)
	ann := e.newUser("ann@example.com")
	acme, err := e.create(ann.ID, "Acme", "")
	require.NoError(t, err)
	e.putOnTrialWithAnOpenInvoice(acme.ID)

	require.NoError(t, e.deleteUser(ann.ID))

	assert.True(t, e.orgDeleted(acme.ID))
	got := e.subscriptionEnd(acme.ID)
	assert.Equal(t, "canceled", got.status)
	assert.Equal(t, "void", got.invoiceStatus)
}

func TestGuard_AMemberLeavingLeavesTheSubscriptionAlone(t *testing.T) {
	e := newEnv(t)
	orgID, _, member, _ := e.teamOf()
	e.putOnTrialWithAnOpenInvoice(orgID)

	require.NoError(t, e.deleteUser(member))

	got := e.subscriptionEnd(orgID)
	assert.Equal(t, "trialing", got.status)
	assert.Equal(t, "open", got.invoiceStatus)
}

// failingLifecycle starts Free subscriptions like the real one and refuses to end them.
type failingLifecycle struct{ org.SubscriptionLifecycle }

func (failingLifecycle) OrganizationDeleted(context.Context, db.DBTX, uuid.UUID) error {
	return errors.New("billing is down")
}

// The organization and its subscription end together or not at all.
func TestDelete_IfTheSubscriptionCannotEndTheOrganizationIsNotDeleted(t *testing.T) {
	e := newEnv(t)
	ann := e.newUser("ann@example.com")
	acme, err := e.create(ann.ID, "Acme", "")
	require.NoError(t, err)
	e.putOnTrialWithAnOpenInvoice(acme.ID)
	e.orgs.Subscriptions = failingLifecycle{e.orgs.Subscriptions}
	ctx := org.WithRole(reqctx.WithOrgID(context.Background(), acme.ID), org.RoleOwner)

	err = e.orgs.Delete(ctx)

	require.Error(t, err)
	assert.False(t, e.orgDeleted(acme.ID), "rolled back")
	assert.Equal(t, "trialing", e.subscriptionEnd(acme.ID).status)

	err = e.db.WithTx(context.Background(), func(tx pgx.Tx) error { return e.orgs.BeforeUserDeleted(context.Background(), tx, ann.ID) })
	require.Error(t, err)
	assert.False(t, e.orgDeleted(acme.ID), "account deletion is rolled back with it")
}
