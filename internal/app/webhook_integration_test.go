//go:build integration

package app_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/billing/payment"
	"github.com/kangsolder001/saka/internal/billing/payment/fake"
)

const webhookPath = "/api/v1/webhooks/fake"

// paymentWebhook posts a notification signed the way the fake gateway signs it.
func (s *stack) paymentWebhook(ev payment.WebhookEvent) response {
	s.t.Helper()
	body, header := fake.Webhook(ev)
	headers := map[string]string{}
	for k := range header {
		headers[k] = header.Get(k)
	}
	return s.do(http.MethodPost, webhookPath, string(body), headers)
}

func paid(eventID, number string, amount int64) payment.WebhookEvent {
	return payment.WebhookEvent{
		EventID: eventID, InvoiceNumber: number, ProviderRef: "txn-" + eventID, State: payment.StatePaid, Amount: amount, Currency: "IDR",
	}
}

func (s *stack) paymentEvents() int {
	s.t.Helper()
	var n int
	require.NoError(s.t, s.queryRow("SELECT count(*) FROM payment_events").Scan(&n))
	return n
}

func TestWebhook_EndToEnd_ASignatureThatDoesNotVerifyIs401AndStoresNothing(t *testing.T) {
	s := newStack(t)
	body, _ := fake.Webhook(paid("evt-1", "INV-2026-000001", 99000))

	for name, headers := range map[string]map[string]string{
		"no signature":    nil,
		"wrong signature": {fake.SignatureHeader: fake.Sign([]byte("other"))},
		"not hex":         {fake.SignatureHeader: "zz"},
	} {
		res := s.do(http.MethodPost, webhookPath, string(body), headers)

		assert.Equal(t, http.StatusUnauthorized, res.Code, name)
		assert.Equal(t, "UNAUTHENTICATED", errorOf(res)["code"], name)
	}
	assert.Zero(t, s.paymentEvents())
}

func TestWebhook_EndToEnd_IsPublicAndAnswers200ForAVerifiedEvent(t *testing.T) {
	s := newStack(t)

	res := s.paymentWebhook(paid("evt-1", "INV-2026-000001", 99000)) // no login: the signature is the credential

	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	assert.Equal(t, 1, s.paymentEvents())
}

func TestWebhook_EndToEnd_ARepeatedDeliveryIsAnsweredOKAndStoredOnce(t *testing.T) {
	s := newStack(t)
	ev := paid("evt-1", "INV-2026-000001", 99000)

	assert.Equal(t, http.StatusOK, s.paymentWebhook(ev).Code)
	assert.Equal(t, http.StatusOK, s.paymentWebhook(ev).Code)

	assert.Equal(t, 1, s.paymentEvents())
}

func TestWebhook_EndToEnd_UnknownProvidersAndBadBodies(t *testing.T) {
	s := newStack(t)
	body, header := fake.Webhook(paid("evt-1", "INV-1", 1))
	headers := map[string]string{fake.SignatureHeader: header.Get(fake.SignatureHeader)}

	assert.Equal(t, http.StatusNotFound, s.do(http.MethodPost, "/api/v1/webhooks/xendit", string(body), headers).Code)

	bad := []byte(`{"hello":"world"}`)
	res := s.do(http.MethodPost, webhookPath, string(bad), map[string]string{fake.SignatureHeader: fake.Sign(bad)})
	assert.Equal(t, http.StatusBadRequest, res.Code, res.Body.String())

	assert.Equal(t, http.StatusNotFound, s.do(http.MethodGet, webhookPath, "", nil).Code, "only POST")
	assert.Zero(t, s.paymentEvents())
}

// buyPlan buys a plan as the owner of a Free organization and returns
// the invoice number.
func (s *stack) buyPlan(tm team, plan string) string {
	s.t.Helper()
	res := s.checkout(tm.owner, tm, plan)
	require.Equal(s.t, http.StatusCreated, res.Code, res.Body.String())
	return invoiceOf(res)["number"].(string)
}

func TestWebhook_EndToEnd_PayingTheInvoiceActivatesThePlanAndConfirmsByEmail(t *testing.T) {
	s := newStack(t)
	tm := s.billingTeam()
	number := s.buyPlan(tm, "pro")
	s.clock.Advance(24 * time.Hour)

	res := s.paymentWebhook(paid("evt-1", number, 99000))

	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	sub := subscriptionOf(s.get(tm.subscriptionPath(), bearer(s.loggedIn("owner@example.com"))))
	assert.Equal(t, "active", sub["status"])
	assert.Equal(t, "pro", sub["effective_plan"])
	assert.Equal(t, "2026-06-02T10:00:00Z", sub["current_period_start"], "the period starts when the payment was confirmed")
	assert.Equal(t, "2026-07-02T10:00:00Z", sub["current_period_end"])
	invoice := s.get(tm.invoicesPath(), bearer(s.loggedIn("owner@example.com"))).json()["items"].([]any)[0].(map[string]any)
	assert.Equal(t, "paid", invoice["status"])
	assert.Nil(t, invoice["payment_url"], "a paid invoice has nothing left to pay")
	assert.Equal(t, 1, s.emailsTo("owner@example.com", "payment_succeeded"))
}

func TestWebhook_EndToEnd_ARedeliveryDoesNotPayTwice(t *testing.T) {
	s := newStack(t)
	tm := s.billingTeam()
	number := s.buyPlan(tm, "pro")
	ev := paid("evt-1", number, 99000)

	assert.Equal(t, http.StatusOK, s.paymentWebhook(ev).Code)
	s.clock.Advance(time.Hour)
	assert.Equal(t, http.StatusOK, s.paymentWebhook(ev).Code)

	assert.Equal(t, 1, s.emailsTo("owner@example.com", "payment_succeeded"))
	var start string
	require.NoError(t, s.queryRow("SELECT to_char(current_period_start AT TIME ZONE 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS') FROM subscriptions WHERE organization_id = $1", tm.orgID).Scan(&start))
	assert.Equal(t, "2026-06-01T10:00:00", start, "the second delivery did not restart the period")
}

func TestWebhook_EndToEnd_ThePaidAmountIsCheckedAgainstTheInvoice(t *testing.T) {
	s := newStack(t)
	tm := s.billingTeam()
	number := s.buyPlan(tm, "pro")

	res := s.paymentWebhook(paid("evt-1", number, 1000))

	require.Equal(t, http.StatusOK, res.Code, "flagged, not retried")
	_, status := s.subscriptionOf(tm.orgID)
	assert.Equal(t, "trialing", status, "a payment of the wrong amount activates nothing")
	var invoiceStatus string
	var note *string
	require.NoError(t, s.queryRow("SELECT (SELECT status FROM invoices WHERE number = $1), (SELECT error FROM payment_events)", number).Scan(&invoiceStatus, &note))
	assert.Equal(t, "open", invoiceStatus)
	require.NotNil(t, note)
	assert.Contains(t, *note, "amount mismatch")
}

func TestWebhook_EndToEnd_AProcessingFailureAnswers500AndTheRetrySucceeds(t *testing.T) {
	s := newStack(t)
	tm := s.billingTeam()
	number := s.buyPlan(tm, "pro")
	_, err := s.admin.Exec(s.ctx(), "UPDATE users SET email = 'not-an-address' WHERE email = 'owner@example.com'")
	require.NoError(t, err)
	s.invalidateAllUsers()
	ev := paid("evt-1", number, 99000)

	failed := s.paymentWebhook(ev)

	require.Equal(t, http.StatusInternalServerError, failed.Code, "the gateway retries on a 500")
	assert.Equal(t, "INTERNAL_ERROR", errorOf(failed)["code"])
	var processed *string
	var errText *string
	require.NoError(t, s.queryRow("SELECT processed_at::text, error FROM payment_events").Scan(&processed, &errText))
	assert.Nil(t, processed)
	assert.NotNil(t, errText)

	_, err = s.admin.Exec(s.ctx(), "UPDATE users SET email = 'owner@example.com' WHERE email = 'not-an-address'")
	require.NoError(t, err)
	s.invalidateAllUsers()
	retried := s.paymentWebhook(ev)

	require.Equal(t, http.StatusOK, retried.Code, retried.Body.String())
	_, status := s.subscriptionOf(tm.orgID)
	assert.Equal(t, "active", status)
}
