//go:build integration

package app_test

import (
	"net/http"
	"testing"

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
