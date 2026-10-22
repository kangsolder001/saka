//go:build integration

package app_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// subscriptionAndInvoice returns the status of the organization's subscription
// and of its (single) invoice.
func (s *stack) subscriptionAndInvoice(orgID string) (subscription, invoice string) {
	s.t.Helper()
	require.NoError(s.t, s.queryRow(`
		SELECT (SELECT status FROM subscriptions WHERE organization_id = $1), (SELECT status FROM invoices WHERE organization_id = $1)`, orgID).
		Scan(&subscription, &invoice))
	return subscription, invoice
}

func TestDeleteOrg_EndToEnd_CancelsTheSubscriptionAndVoidsTheOpenInvoice(t *testing.T) {
	s := newStack(t)
	tm := s.billingTeam()
	number := s.buyPlan(tm, "pro")
	sub, inv := s.subscriptionAndInvoice(tm.orgID)
	require.Equal(t, "trialing", sub)
	require.Equal(t, "open", inv)

	res := s.do(http.MethodDelete, orgPath(tm.orgID), "", bearer(tm.owner))

	require.Equal(t, http.StatusNoContent, res.Code, res.Body.String())
	sub, inv = s.subscriptionAndInvoice(tm.orgID)
	assert.Equal(t, "canceled", sub)
	assert.Equal(t, "void", inv)

	// Someone pays the old link afterwards: recorded and flagged, never activated.
	require.Equal(t, http.StatusOK, s.paymentWebhook(paid("evt-1", number, 99000)).Code)
	sub, inv = s.subscriptionAndInvoice(tm.orgID)
	assert.Equal(t, "canceled", sub)
	assert.Equal(t, "void", inv)
	var note *string
	require.NoError(t, s.queryRow("SELECT error FROM payment_events").Scan(&note))
	require.NotNil(t, note, "left for a person to refund")
}

func TestDeleteAccount_EndToEnd_AlsoEndsTheSubscriptionOfTheOrganizationItDeletes(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	s.verify("ann@example.com")
	access := s.loggedIn("ann@example.com")
	orgID := s.firstOrg("ann@example.com")
	tm := team{orgID: orgID, owner: access}
	s.buyPlan(tm, "pro")

	res := s.deleteMe(access, "correct-horse")

	require.Equal(t, http.StatusNoContent, res.Code, res.Body.String())
	sub, inv := s.subscriptionAndInvoice(orgID)
	assert.Equal(t, "canceled", sub)
	assert.Equal(t, "void", inv)
}
