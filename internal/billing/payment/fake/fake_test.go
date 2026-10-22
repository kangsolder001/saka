package fake_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/billing/payment"
	"github.com/kangsolder001/saka/internal/billing/payment/fake"
)

func request(body []byte, header http.Header) *http.Request {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/webhooks/fake", bytes.NewReader(body))
	r.Header = header
	return r
}

func sampleEvent() payment.WebhookEvent {
	return payment.WebhookEvent{
		EventID: "evt-1", InvoiceNumber: "INV-2026-000001", ProviderRef: "ref-1",
		State: payment.StatePaid, Amount: 99000, Currency: "IDR",
	}
}

func TestCreatePayment_IsDeterministicAndRecorded(t *testing.T) {
	p := fake.New()
	req := payment.PaymentRequest{InvoiceNumber: "INV-2026-000001", Amount: 99000, Currency: "IDR"}

	session, err := p.CreatePayment(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, fake.PaymentURL("INV-2026-000001"), session.URL)
	assert.Equal(t, "INV-2026-000001", session.Ref)
	assert.Equal(t, []payment.PaymentRequest{req}, p.Created())
	last, err := p.Last()
	require.NoError(t, err)
	assert.Equal(t, req, last)
}

func TestCreatePayment_AReusedInvoiceNumberIsRefusedLikeARealGateway(t *testing.T) {
	p := fake.New()
	req := payment.PaymentRequest{InvoiceNumber: "INV-1", Amount: 1, Currency: "IDR"}
	_, err := p.CreatePayment(context.Background(), req)
	require.NoError(t, err)

	_, err = p.CreatePayment(context.Background(), req)

	var gw *payment.GatewayError
	assert.ErrorAs(t, err, &gw)
}

func TestFailWith_MakesTheGatewayUnreachableUntilCleared(t *testing.T) {
	p := fake.New()
	p.FailWith(errors.New("boom"))

	_, err := p.CreatePayment(context.Background(), payment.PaymentRequest{InvoiceNumber: "INV-1"})
	var gw *payment.GatewayError
	require.ErrorAs(t, err, &gw)
	_, err = p.GetPaymentStatus(context.Background(), "INV-1")
	require.ErrorAs(t, err, &gw)

	p.FailWith(nil)
	_, err = p.CreatePayment(context.Background(), payment.PaymentRequest{InvoiceNumber: "INV-1"})
	assert.NoError(t, err)
}

func TestVerifyWebhook_RoundTripsASignedEvent(t *testing.T) {
	body, header := fake.Webhook(sampleEvent())

	ev, err := fake.New().VerifyWebhook(request(body, header))

	require.NoError(t, err)
	want := sampleEvent()
	want.Raw = body
	assert.Equal(t, want, ev)
}

func TestVerifyWebhook_RejectsABadOrMissingSignature(t *testing.T) {
	body, header := fake.Webhook(sampleEvent())
	tampered := bytes.Replace(body, []byte("99000"), []byte("1"), 1)
	noSignature := http.Header{}

	for name, r := range map[string]*http.Request{
		"tampered body":  request(tampered, header),
		"no signature":   request(body, noSignature),
		"not even hex":   request(body, http.Header{fake.SignatureHeader: {"zz"}}),
		"empty body":     request(nil, header),
		"another secret": request(body, http.Header{fake.SignatureHeader: {fake.Sign([]byte("other"))}}),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := fake.New().VerifyWebhook(r)

			assert.ErrorIs(t, err, payment.ErrInvalidSignature)
		})
	}
}

func TestVerifyWebhook_ASignedBodyThatIsNotAnEventIsMalformed(t *testing.T) {
	body := []byte(`{"hello":"world"}`)

	_, err := fake.New().VerifyWebhook(request(body, http.Header{fake.SignatureHeader: {fake.Sign(body)}}))

	assert.ErrorIs(t, err, payment.ErrMalformedEvent)
}

func TestGetPaymentStatus_ReportsWhatTheTestSet(t *testing.T) {
	p := fake.New()
	_, err := p.CreatePayment(context.Background(), payment.PaymentRequest{InvoiceNumber: "INV-1", Amount: 5000, Currency: "IDR"})
	require.NoError(t, err)

	st, err := p.GetPaymentStatus(context.Background(), "INV-1")
	require.NoError(t, err)
	assert.Equal(t, payment.StatePending, st.State)
	assert.Equal(t, int64(5000), st.Amount)

	p.SetState("INV-1", payment.StatePaid)
	st, err = p.GetPaymentStatus(context.Background(), "INV-1")
	require.NoError(t, err)
	assert.Equal(t, payment.StatePaid, st.State)

	_, err = p.GetPaymentStatus(context.Background(), "INV-404")
	assert.ErrorIs(t, err, payment.ErrNotFound)
}

func TestNewRegistry_RefusesTwoProvidersWithOneName(t *testing.T) {
	_, err := payment.NewRegistry(fake.New(), fake.New())
	require.Error(t, err)

	reg, err := payment.NewRegistry(fake.New())
	require.NoError(t, err)
	assert.Contains(t, reg, "fake")
}
