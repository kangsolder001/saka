//go:build integration

package billing_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/billing/payment"
	"github.com/kangsolder001/saka/internal/billing/payment/fake"
	"github.com/kangsolder001/saka/internal/platform/apperr"
)

// webhookRequest builds a notification as the fake gateway would send it.
func webhookRequest(ev payment.WebhookEvent) *http.Request {
	body, header := fake.Webhook(ev)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/webhooks/fake", bytes.NewReader(body))
	r.Header = header
	return r
}

// deliver hands a signed notification to the service.
func (e *payEnv) deliver(ev payment.WebhookEvent) error {
	return e.svc.HandleWebhook(context.Background(), fake.Name, webhookRequest(ev))
}

// eventRow is a payment_events row.
type eventRow struct {
	EventID     string
	Provider    string
	ProcessedAt *time.Time
	Error       *string
}

func (e *payEnv) events() []eventRow {
	e.t.Helper()
	rows, err := e.admin.Query(context.Background(), "SELECT event_id, provider, processed_at, error FROM payment_events ORDER BY received_at, event_id")
	require.NoError(e.t, err)
	defer rows.Close()
	var out []eventRow
	for rows.Next() {
		var r eventRow
		require.NoError(e.t, rows.Scan(&r.EventID, &r.Provider, &r.ProcessedAt, &r.Error))
		out = append(out, r)
	}
	require.NoError(e.t, rows.Err())
	return out
}

func paidEvent(id, number string, amount int64) payment.WebhookEvent {
	return payment.WebhookEvent{
		EventID: id, InvoiceNumber: number, ProviderRef: "txn-" + id, State: payment.StatePaid, Amount: amount, Currency: "IDR",
	}
}

func TestWebhook_ASignatureThatDoesNotVerifyIsRejectedAndNothingIsStored(t *testing.T) {
	e := newPayEnv(t)
	body, header := fake.Webhook(paidEvent("evt-1", "INV-2026-000001", 99000))
	header.Set(fake.SignatureHeader, fake.Sign([]byte("something else")))
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/webhooks/fake", bytes.NewReader(body))
	r.Header = header

	err := e.svc.HandleWebhook(context.Background(), fake.Name, r)

	require.Error(t, err)
	assert.True(t, apperr.IsCode(err, apperr.CodeUnauthenticated), err.Error())
	assert.Empty(t, e.events(), "an unauthenticated notification leaves no trace in the table")
}

func TestWebhook_ABodyThatIsNotANotificationIsAValidationError(t *testing.T) {
	e := newPayEnv(t)
	body := []byte(`{"hello":"world"}`)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/webhooks/fake", bytes.NewReader(body))
	r.Header.Set(fake.SignatureHeader, fake.Sign(body))

	err := e.svc.HandleWebhook(context.Background(), fake.Name, r)

	assert.True(t, apperr.IsCode(err, apperr.CodeValidation), "%v", err)
	assert.Empty(t, e.events())
}

func TestWebhook_AnUnknownProviderIsNotFound(t *testing.T) {
	e := newPayEnv(t)

	err := e.svc.HandleWebhook(context.Background(), "xendit", webhookRequest(paidEvent("evt-1", "INV-1", 1)))

	assert.True(t, apperr.IsCode(err, apperr.CodeNotFound), "%v", err)
	assert.Empty(t, e.events())
}

func TestWebhook_AVerifiedEventIsStoredWithItsPayload(t *testing.T) {
	e := newPayEnv(t)

	require.NoError(t, e.deliver(paidEvent("evt-1", "INV-2026-000001", 99000)))

	events := e.events()
	require.Len(t, events, 1)
	assert.Equal(t, "evt-1", events[0].EventID)
	assert.Equal(t, "fake", events[0].Provider)
	var invoice string
	require.NoError(t, e.admin.QueryRow(context.Background(), "SELECT payload->>'invoice_number' FROM payment_events").Scan(&invoice))
	assert.Equal(t, "INV-2026-000001", invoice, "the raw notification is kept as received")
}

func TestWebhook_TheSameEventDeliveredTwiceIsStoredOnce(t *testing.T) {
	e := newPayEnv(t)
	ev := paidEvent("evt-1", "INV-2026-000001", 99000)

	require.NoError(t, e.deliver(ev))
	require.NoError(t, e.deliver(ev), "the repeat is answered with success, so the gateway stops retrying")

	assert.Len(t, e.events(), 1)
}

func TestWebhook_DeliveriesOfOneEventAtTheSameTimeAreStoredOnce(t *testing.T) {
	e := newPayEnv(t)
	ev := paidEvent("evt-1", "INV-2026-000001", 99000)
	const deliveries = 8
	errs := make([]error, deliveries)
	var wg sync.WaitGroup
	for i := range deliveries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = e.deliver(ev)
		}()
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	assert.Len(t, e.events(), 1)
}

func TestWebhook_EventIDsArePerProvider(t *testing.T) {
	e := newPayEnv(t)
	require.NoError(t, e.deliver(paidEvent("evt-1", "INV-2026-000001", 99000)))
	_, err := e.admin.Exec(context.Background(),
		"INSERT INTO payment_events (provider, event_id, payload) VALUES ('midtrans', 'evt-1', '{}')")
	require.NoError(t, err, "another provider may reuse an event id")

	assert.Len(t, e.events(), 2)
}
