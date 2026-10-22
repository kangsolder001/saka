//go:build integration

package app_test

import (
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/billing/payment/fake"
)

func (tm team) checkoutPath() string { return orgPath(tm.orgID) + "/subscription/checkout" }
func (tm team) invoicesPath() string { return orgPath(tm.orgID) + "/invoices" }

func checkoutBody(plan string) string { return `{"plan":"` + plan + `"}` }

// billingTeam is a team (owner, admin, member, outsider) whose owner and admin
// verified their email, still on the Free plan.
func (s *stack) billingTeam() team {
	s.t.Helper()
	tm := s.newTeam()
	s.verify("owner@example.com", "admin@example.com")
	return tm
}

func invoiceOf(res response) map[string]any {
	return res.json()["invoice"].(map[string]any)
}

func (s *stack) checkout(access string, tm team, plan string) response {
	s.t.Helper()
	return s.post(tm.checkoutPath(), checkoutBody(plan), bearer(access))
}

func TestCheckout_EndToEnd_TheOwnerBuysAPlanAndGetsAnInvoiceWithAPaymentLink(t *testing.T) {
	s := newStack(t)
	tm := s.billingTeam()

	res := s.checkout(tm.owner, tm, "pro")

	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	body := res.json()
	inv := invoiceOf(res)
	assert.Equal(t, "INV-2026-000001", inv["number"])
	assert.Equal(t, "checkout", inv["kind"])
	assert.Equal(t, "pro", inv["plan"])
	assert.Equal(t, "Pro", inv["plan_name"])
	assert.Equal(t, 99000.0, inv["amount"])
	assert.Equal(t, "IDR", inv["currency"])
	assert.Equal(t, "open", inv["status"])
	assert.Equal(t, fake.PaymentURL("INV-2026-000001"), inv["payment_url"])
	assert.Nil(t, inv["paid_at"])
	assert.Equal(t, true, body["trial_started"], "a first checkout of a plan with a trial starts the trial")

	sub := body["subscription"].(map[string]any)
	assert.Equal(t, "trialing", sub["status"])
	assert.Equal(t, "pro", sub["plan"])
	assert.Equal(t, "pro", sub["effective_plan"], "the trial gives access to the plan right away")
	assert.Equal(t, "2026-06-15T10:00:00Z", sub["trial_ends_at"])

	assert.Equal(t, 1, s.emailsTo("owner@example.com", "invoice_created"), "the invoice is emailed through the outbox")
	plan, status := s.subscriptionOf(tm.orgID)
	assert.Equal(t, "pro", plan)
	assert.Equal(t, "trialing", status)
}

func TestCheckout_EndToEnd_TheInvoiceCanBeListedAndReadBack(t *testing.T) {
	s := newStack(t)
	tm := s.billingTeam()
	created := invoiceOf(s.checkout(tm.owner, tm, "pro"))

	list := s.get(tm.invoicesPath(), bearer(tm.owner))
	one := s.get(tm.invoicesPath()+"/"+created["id"].(string), bearer(tm.owner))

	require.Equal(t, http.StatusOK, list.Code, list.Body.String())
	items := list.json()["items"].([]any)
	require.Len(t, items, 1)
	assert.Equal(t, created, items[0])
	assert.Nil(t, list.json()["next_cursor"])
	require.Equal(t, http.StatusOK, one.Code, one.Body.String())
	assert.Equal(t, created, invoiceOf(one))
}

func TestCheckout_EndToEnd_CallingItAgainReturnsTheSameInvoice(t *testing.T) {
	s := newStack(t)
	tm := s.billingTeam()
	first := s.checkout(tm.owner, tm, "pro")
	require.Equal(t, http.StatusCreated, first.Code)

	second := s.checkout(tm.owner, tm, "pro")

	require.Equal(t, http.StatusOK, second.Code, second.Body.String())
	assert.Equal(t, invoiceOf(first), invoiceOf(second))
	assert.Equal(t, false, second.json()["trial_started"])
	assert.Equal(t, 1, s.emailsTo("owner@example.com", "invoice_created"))
	assert.Len(t, s.gateway.Created(), 1)
}

func TestCheckout_EndToEnd_ThePriceComesFromThePlanNotTheRequest(t *testing.T) {
	s := newStack(t)
	tm := s.billingTeam()

	// An attempt to name the amount is an unknown field and is refused.
	res := s.post(tm.checkoutPath(), `{"plan":"pro","amount":1,"currency":"USD"}`, bearer(tm.owner))

	require.Equal(t, http.StatusBadRequest, res.Code, res.Body.String())
	assert.Empty(t, s.gateway.Created())

	s.checkout(tm.owner, tm, "business")
	req, err := s.gateway.Last()
	require.NoError(t, err)
	assert.Equal(t, int64(299000), req.Amount)
}

func TestCheckout_EndToEnd_BadPlansAreValidationErrors(t *testing.T) {
	s := newStack(t)
	tm := s.billingTeam()

	for name, body := range map[string]string{
		"missing": `{}`, "empty": `{"plan":""}`, "unknown": `{"plan":"platinum"}`, "free": `{"plan":"free"}`, "wrong type": `{"plan":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			res := s.post(tm.checkoutPath(), body, bearer(tm.owner))

			require.Equal(t, http.StatusBadRequest, res.Code, res.Body.String())
			assert.Equal(t, "VALIDATION_ERROR", errorOf(res)["code"])
		})
	}
	assert.Empty(t, s.gateway.Created())
}

func TestCheckout_EndToEnd_OnlyAVerifiedOwnerMayBuy(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam() // nobody verified yet

	unverified := s.checkout(tm.owner, tm, "pro")
	assert.Equal(t, http.StatusForbidden, unverified.Code)
	assert.Equal(t, "EMAIL_NOT_VERIFIED", errorOf(unverified)["code"])

	s.verify("owner@example.com", "admin@example.com")
	for name, token := range map[string]string{"admin": tm.admin, "member": tm.member} {
		res := s.checkout(token, tm, "pro")
		assert.Equal(t, http.StatusForbidden, res.Code, name)
		assert.Equal(t, "FORBIDDEN", errorOf(res)["code"], "%s: even a verified admin cannot manage billing", name)
	}
	assert.Equal(t, http.StatusNotFound, s.checkout(tm.outside, tm, "pro").Code, "a non-member does not learn the organization exists")
	assert.Equal(t, http.StatusUnauthorized, s.post(tm.checkoutPath(), checkoutBody("pro")).Code)
	assert.Empty(t, s.gateway.Created())
	assert.Zero(t, s.emailsTo("owner@example.com", "invoice_created"))

	assert.Equal(t, http.StatusCreated, s.checkout(tm.owner, tm, "pro").Code)
}

func TestCheckout_EndToEnd_AnOrganizationAlreadyOnThePlanGetsAConflict(t *testing.T) {
	s := newStack(t)
	tm := s.billingTeam()
	s.setPlan(tm.orgID, "pro", "active")
	_, err := s.admin.Exec(s.ctx(), "UPDATE subscriptions SET current_period_end = now() + interval '20 days' WHERE organization_id = $1", tm.orgID)
	require.NoError(t, err)

	res := s.checkout(tm.owner, tm, "pro")

	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
	assert.Equal(t, "CONFLICT", errorOf(res)["code"])
	assert.Equal(t, "already_subscribed", detailsOf(res)["reason"])
}

func TestCheckout_EndToEnd_AGatewayOutageIsA502AndLeavesNothingBehind(t *testing.T) {
	s := newStack(t)
	tm := s.billingTeam()
	s.gateway.FailWith(errors.New("tcp connect: connection refused"))

	res := s.checkout(tm.owner, tm, "pro")

	require.Equal(t, http.StatusBadGateway, res.Code, res.Body.String())
	assert.Equal(t, "PAYMENT_PROVIDER_ERROR", errorOf(res)["code"])
	assert.NotContains(t, res.Body.String(), "connection refused", "the cause stays in the log")
	plan, status := s.subscriptionOf(tm.orgID)
	assert.Equal(t, "free", plan)
	assert.Equal(t, "active", status)
	var invoices int
	require.NoError(t, s.queryRow("SELECT count(*) FROM invoices").Scan(&invoices))
	assert.Zero(t, invoices)
	assert.Zero(t, s.emailsTo("owner@example.com", "invoice_created"))
}

func TestInvoices_EndToEnd_OnlyTheOwnerSeesThemAndNobodyElsesInvoices(t *testing.T) {
	s := newStack(t)
	tm := s.billingTeam()
	created := invoiceOf(s.checkout(tm.owner, tm, "pro"))
	id := created["id"].(string)

	for name, token := range map[string]string{"admin": tm.admin, "member": tm.member} {
		assert.Equal(t, http.StatusForbidden, s.get(tm.invoicesPath(), bearer(token)).Code, name)
		assert.Equal(t, http.StatusForbidden, s.get(tm.invoicesPath()+"/"+id, bearer(token)).Code, name)
	}
	assert.Equal(t, http.StatusNotFound, s.get(tm.invoicesPath(), bearer(tm.outside)).Code)

	// Another organization's owner reads neither the list nor the invoice by id.
	s.registered("bella@example.com")
	bella := s.loggedIn("bella@example.com")
	b := team{orgID: s.firstOrg("bella@example.com"), owner: bella}
	assert.Equal(t, http.StatusNotFound, s.get(tm.invoicesPath()+"/"+id, bearer(bella)).Code, "A's invoice through A's URL")
	assert.Equal(t, http.StatusNotFound, s.get(b.invoicesPath()+"/"+id, bearer(bella)).Code, "A's invoice id inside B's tenant")
	own := s.get(b.invoicesPath(), bearer(bella))
	require.Equal(t, http.StatusOK, own.Code)
	assert.Empty(t, own.json()["items"])
	assert.Equal(t, http.StatusNotFound, s.get(tm.invoicesPath()+"/not-a-uuid", bearer(tm.owner)).Code)
}

func TestInvoices_EndToEnd_ListIsPaginatedNewestFirst(t *testing.T) {
	s := newStack(t)
	tm := s.billingTeam()
	past := "2026-03-01T00:00:00Z"
	_, err := s.admin.Exec(s.ctx(), "UPDATE subscriptions SET trial_ends_at = $2 WHERE organization_id = $1", tm.orgID, past) // trial used
	require.NoError(t, err)
	var numbers []string
	for _, plan := range []string{"pro", "business", "pro"} {
		numbers = append(numbers, invoiceOf(s.checkout(tm.owner, tm, plan))["number"].(string))
		s.clock.Advance(1)
	}

	first := s.get(tm.invoicesPath()+"?limit=2", bearer(tm.owner))
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	items := first.json()["items"].([]any)
	require.Len(t, items, 2)
	assert.Equal(t, numbers[2], items[0].(map[string]any)["number"])
	assert.Equal(t, numbers[1], items[1].(map[string]any)["number"])
	cursor, ok := first.json()["next_cursor"].(string)
	require.True(t, ok)

	second := s.get(tm.invoicesPath()+"?limit=2&cursor="+url.QueryEscape(cursor), bearer(tm.owner))
	require.Equal(t, http.StatusOK, second.Code)
	rest := second.json()["items"].([]any)
	require.Len(t, rest, 1)
	assert.Equal(t, numbers[0], rest[0].(map[string]any)["number"])
	assert.Nil(t, second.json()["next_cursor"])

	assert.Equal(t, http.StatusBadRequest, s.get(tm.invoicesPath()+"?limit=0", bearer(tm.owner)).Code)
	assert.Equal(t, http.StatusBadRequest, s.get(tm.invoicesPath()+"?cursor=garbage", bearer(tm.owner)).Code)
}
