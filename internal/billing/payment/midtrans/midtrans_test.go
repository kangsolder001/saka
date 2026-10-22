package midtrans_test

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/billing/payment"
	"github.com/kangsolder001/saka/internal/billing/payment/midtrans"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

const serverKey = "SB-Mid-server-TESTKEY"

// Signatures computed outside the code under test:
// sha512(order_id + status_code + gross_amount + serverKey), in hex.
const (
	sigInvoice1Paid    = "a1cb8777ea2bbe93be7fb71798d3725e4196926f09f1b81a0c4de4a40168569e1f34624ff764e6a5d2587305a398db52d189c4905ec82a04339ae9decb6e5bfb" // INV-2026-000001 200 99000.00
	sigInvoice1NumberP = "b9016967adfe3ece649c93e05d48b1333a652edb9548d422caaae403a98cb2c518625c5fc50526fb8d164d3cff5d71c83943f880c21a6bf5bd930dfd7e77e488" // INV-2026-000001 200 99000
	sigInvoice2Expired = "e577dfedaebb5ddbe463b5793edd2435fdca7bd6f8f609bcb16bb21b0aa454148649bf215ccb5ddebec4d01321a5cbdf41605634a054b6807ab620b686cba6ea" // INV-2026-000002 407 299000.00
	basicAuth          = "Basic U0ItTWlkLXNlcnZlci1URVNUS0VZOg=="                                                                                           // base64("SB-Mid-server-TESTKEY:")
)

var now = time.Date(2026, 6, 1, 3, 0, 0, 0, time.UTC) // 10:00 in Jakarta

func newProvider(t *testing.T, snap, api string) *midtrans.Provider {
	t.Helper()
	p, err := midtrans.New(midtrans.Config{ServerKey: serverKey, SnapBaseURL: snap, APIBaseURL: api}, clock.NewFake(now))
	require.NoError(t, err)
	return p
}

func webhook(body string) *http.Request {
	return httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/webhooks/midtrans", strings.NewReader(body))
}

func settlementBody(signature string) string {
	return `{"transaction_id":"txn-1","transaction_status":"settlement","status_code":"200","order_id":"INV-2026-000001",` +
		`"gross_amount":"99000.00","payment_type":"bank_transfer","currency":"IDR","fraud_status":"accept","signature_key":"` + signature + `"}`
}

func TestNew_RequiresAServerKeyAndUsableURLs(t *testing.T) {
	_, err := midtrans.New(midtrans.Config{SnapBaseURL: midtrans.SandboxSnapBaseURL, APIBaseURL: midtrans.SandboxAPIBaseURL}, clock.NewFake(now))
	require.Error(t, err)

	_, err = midtrans.New(midtrans.Config{ServerKey: serverKey, SnapBaseURL: "not a url", APIBaseURL: midtrans.SandboxAPIBaseURL}, clock.NewFake(now))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), serverKey)

	_, err = midtrans.New(midtrans.Config{ServerKey: serverKey, SnapBaseURL: midtrans.SandboxSnapBaseURL, APIBaseURL: midtrans.SandboxAPIBaseURL}, clock.NewFake(now))
	require.NoError(t, err)
}

func TestVerifyWebhook_AcceptsAValidSignatureAndNormalizesTheEvent(t *testing.T) {
	p := newProvider(t, midtrans.SandboxSnapBaseURL, midtrans.SandboxAPIBaseURL)
	body := settlementBody(sigInvoice1Paid)

	ev, err := p.VerifyWebhook(webhook(body))

	require.NoError(t, err)
	assert.Equal(t, "txn-1:settlement:200:accept", ev.EventID)
	assert.Equal(t, "INV-2026-000001", ev.InvoiceNumber)
	assert.Equal(t, "txn-1", ev.ProviderRef)
	assert.Equal(t, payment.StatePaid, ev.State)
	assert.Equal(t, int64(99000), ev.Amount)
	assert.Equal(t, "IDR", ev.Currency)
	assert.JSONEq(t, body, string(ev.Raw))
}

func TestVerifyWebhook_TheSameNotificationAlwaysHasTheSameEventID(t *testing.T) {
	p := newProvider(t, midtrans.SandboxSnapBaseURL, midtrans.SandboxAPIBaseURL)

	first, err := p.VerifyWebhook(webhook(settlementBody(sigInvoice1Paid)))
	require.NoError(t, err)
	second, err := p.VerifyWebhook(webhook(settlementBody(sigInvoice1Paid)))
	require.NoError(t, err)

	assert.Equal(t, first.EventID, second.EventID)
}

func TestVerifyWebhook_RejectsASignatureThatDoesNotMatch(t *testing.T) {
	p := newProvider(t, midtrans.SandboxSnapBaseURL, midtrans.SandboxAPIBaseURL)
	cases := map[string]string{
		"another notification's signature": settlementBody(sigInvoice2Expired),
		"the amount as an integer":         settlementBody(sigInvoice1NumberP),
		"upper case but wrong":             settlementBody(strings.ToUpper(sigInvoice2Expired)),
		"empty":                            settlementBody(""),
		"garbage":                          settlementBody("not-hex"),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := p.VerifyWebhook(webhook(body))

			assert.ErrorIs(t, err, payment.ErrInvalidSignature)
		})
	}
}

func TestVerifyWebhook_ASignatureOverTamperedFieldsFails(t *testing.T) {
	p := newProvider(t, midtrans.SandboxSnapBaseURL, midtrans.SandboxAPIBaseURL)
	// The attacker keeps the real signature but pays less, or points at another order.
	for name, body := range map[string]string{
		"amount":   strings.Replace(settlementBody(sigInvoice1Paid), "99000.00", "1000.00", 1),
		"order id": strings.Replace(settlementBody(sigInvoice1Paid), "INV-2026-000001", "INV-2026-000009", 1),
		"code":     strings.Replace(settlementBody(sigInvoice1Paid), `"status_code":"200"`, `"status_code":"201"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := p.VerifyWebhook(webhook(body))

			assert.ErrorIs(t, err, payment.ErrInvalidSignature)
		})
	}
}

func TestVerifyWebhook_ASignatureMadeWithAnotherServerKeyFails(t *testing.T) {
	other, err := midtrans.New(midtrans.Config{ServerKey: "SB-Mid-server-OTHER", SnapBaseURL: midtrans.SandboxSnapBaseURL, APIBaseURL: midtrans.SandboxAPIBaseURL}, clock.NewFake(now))
	require.NoError(t, err)

	_, err = other.VerifyWebhook(webhook(settlementBody(sigInvoice1Paid)))

	assert.ErrorIs(t, err, payment.ErrInvalidSignature)
}

func TestVerifyWebhook_MalformedBodies(t *testing.T) {
	p := newProvider(t, midtrans.SandboxSnapBaseURL, midtrans.SandboxAPIBaseURL)

	for name, body := range map[string]string{
		"not json": `not json`,
		"array":    `[]`,
		"empty":    ``,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := p.VerifyWebhook(webhook(body))

			assert.ErrorIs(t, err, payment.ErrMalformedEvent)
		})
	}
	t.Run("an unsigned body without an order id", func(t *testing.T) {
		// A body with no signature is simply unauthenticated.
		_, err := p.VerifyWebhook(webhook(`{"transaction_status":"settlement"}`))

		assert.ErrorIs(t, err, payment.ErrInvalidSignature)
	})
}

func TestVerifyWebhook_AFractionalAmountIsRefusedNotRounded(t *testing.T) {
	p := newProvider(t, midtrans.SandboxSnapBaseURL, midtrans.SandboxAPIBaseURL)
	// 99000.50 with a signature valid for that text.
	body := `{"transaction_id":"t","transaction_status":"settlement","status_code":"200","order_id":"INV-X","gross_amount":"99000.50","signature_key":"` +
		sha512Hex("INV-X"+"200"+"99000.50"+serverKey) + `"}`

	_, err := p.VerifyWebhook(webhook(body))

	assert.ErrorIs(t, err, payment.ErrMalformedEvent)
}

func TestVerifyWebhook_NumbersInsteadOfStringsKeepTheirText(t *testing.T) {
	p := newProvider(t, midtrans.SandboxSnapBaseURL, midtrans.SandboxAPIBaseURL)
	// status_code and gross_amount as JSON numbers: the signature covers "200" and "99000".
	body := `{"transaction_id":"t","transaction_status":"settlement","status_code":200,"order_id":"INV-2026-000001","gross_amount":99000,"signature_key":"` +
		sigInvoice1NumberP + `"}`

	ev, err := p.VerifyWebhook(webhook(body))

	require.NoError(t, err)
	assert.Equal(t, int64(99000), ev.Amount)
	assert.Equal(t, "IDR", ev.Currency, "Midtrans only settles rupiah; the field may be missing")
}

func TestVerifyWebhook_TransactionStatusesMapToStates(t *testing.T) {
	p := newProvider(t, midtrans.SandboxSnapBaseURL, midtrans.SandboxAPIBaseURL)
	cases := []struct {
		status, fraud string
		want          payment.State
	}{
		{"settlement", "", payment.StatePaid},
		{"capture", "accept", payment.StatePaid},
		{"capture", "", payment.StatePaid},
		{"capture", "challenge", payment.StatePending},
		{"capture", "deny", payment.StateFailed},
		{"pending", "", payment.StatePending},
		{"authorize", "", payment.StatePending},
		{"deny", "", payment.StateFailed},
		{"failure", "", payment.StateFailed},
		{"cancel", "", payment.StateCanceled},
		{"expire", "", payment.StateExpired},
		{"refund", "", payment.StateRefunded},
		{"partial_refund", "", payment.StateRefunded},
		{"chargeback", "", payment.StateRefunded},
		{"something_new", "", payment.StatePending},
	}
	for _, c := range cases {
		t.Run(c.status+"/"+c.fraud, func(t *testing.T) {
			body := `{"transaction_id":"t","transaction_status":"` + c.status + `","fraud_status":"` + c.fraud +
				`","status_code":"200","order_id":"INV-X","gross_amount":"1000.00","signature_key":"` + sha512Hex("INV-X"+"200"+"1000.00"+serverKey) + `"}`

			ev, err := p.VerifyWebhook(webhook(body))

			require.NoError(t, err)
			assert.Equal(t, c.want, ev.State)
		})
	}
}

func TestVerifyWebhook_CaptureAndSettlementOfOneTransactionAreDifferentEvents(t *testing.T) {
	p := newProvider(t, midtrans.SandboxSnapBaseURL, midtrans.SandboxAPIBaseURL)
	mk := func(status string) payment.WebhookEvent {
		body := `{"transaction_id":"t","transaction_status":"` + status + `","fraud_status":"accept","status_code":"200","order_id":"INV-X","gross_amount":"1000.00","signature_key":"` +
			sha512Hex("INV-X"+"200"+"1000.00"+serverKey) + `"}`
		ev, err := p.VerifyWebhook(webhook(body))
		require.NoError(t, err)
		return ev
	}

	assert.NotEqual(t, mk("capture").EventID, mk("settlement").EventID)
}

// recordedCall is what the fake Midtrans saw.
type recordedCall struct {
	method, path, auth, traceparent, contentType string
	body                                         string
}

type midtransServer struct {
	*httptest.Server
	mu    sync.Mutex
	calls []recordedCall
}

func (m *midtransServer) last() recordedCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[len(m.calls)-1]
}

// newServer answers every request with the given status and body, and records it.
func newServer(t *testing.T, status int, body string) *midtransServer {
	t.Helper()
	m := &midtransServer{}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		m.mu.Lock()
		m.calls = append(m.calls, recordedCall{
			method: r.Method, path: r.URL.Path, auth: r.Header.Get("Authorization"),
			traceparent: r.Header.Get("traceparent"), contentType: r.Header.Get("Content-Type"), body: string(data),
		})
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(m.Close)
	return m
}

func paymentRequest() payment.PaymentRequest {
	return payment.PaymentRequest{
		InvoiceNumber: "INV-2026-000001", Amount: 99000, Currency: "IDR", Description: "Pro - 1 bulan",
		Customer:  payment.Customer{Name: "Ann", Email: "ann@example.com"},
		ExpiresAt: now.Add(72 * time.Hour),
	}
}

func TestCreatePayment_PostsASnapTransactionAndReturnsTheRedirectURL(t *testing.T) {
	srv := newServer(t, http.StatusCreated, `{"token":"tok-123","redirect_url":"https://app.sandbox.midtrans.com/snap/v2/vtweb/tok-123"}`)
	p := newProvider(t, srv.URL, "http://unused.invalid")
	traceID := reqctx.NewTraceID()
	ctx := reqctx.WithTraceID(context.Background(), traceID)

	session, err := p.CreatePayment(ctx, paymentRequest())

	require.NoError(t, err)
	assert.Equal(t, "https://app.sandbox.midtrans.com/snap/v2/vtweb/tok-123", session.URL)
	assert.Equal(t, "INV-2026-000001", session.Ref)

	call := srv.last()
	assert.Equal(t, http.MethodPost, call.method)
	assert.Equal(t, "/snap/v1/transactions", call.path)
	assert.Equal(t, basicAuth, call.auth, "the server key is the basic auth user with an empty password")
	assert.Equal(t, "application/json", call.contentType)
	gotTrace, ok := reqctx.ParseTraceparent(call.traceparent)
	assert.True(t, ok, "the trace id travels to the gateway (spec §11.1): %q", call.traceparent)
	assert.Equal(t, traceID, gotTrace)
	assert.JSONEq(t, `{
		"transaction_details": {"order_id": "INV-2026-000001", "gross_amount": 99000},
		"customer_details": {"first_name": "Ann", "email": "ann@example.com"},
		"item_details": [{"id": "INV-2026-000001", "price": 99000, "quantity": 1, "name": "Pro - 1 bulan"}],
		"expiry": {"start_time": "2026-06-01 03:00:00 +0000", "unit": "minutes", "duration": 4320}
	}`, call.body)
}

func TestCreatePayment_RoundsTheExpiryUpToWholeMinutes(t *testing.T) {
	srv := newServer(t, http.StatusCreated, `{"token":"t","redirect_url":"https://pay.example/t"}`)
	p := newProvider(t, srv.URL, "http://unused.invalid")
	req := paymentRequest()
	req.ExpiresAt = now.Add(90*time.Second + time.Millisecond)

	_, err := p.CreatePayment(context.Background(), req)

	require.NoError(t, err)
	var sent struct {
		Expiry struct {
			Duration int64 `json:"duration"`
		} `json:"expiry"`
	}
	require.NoError(t, json.Unmarshal([]byte(srv.last().body), &sent))
	assert.Equal(t, int64(2), sent.Expiry.Duration)
}

func TestCreatePayment_AnErrorFromMidtransIsAGatewayErrorWithoutSecrets(t *testing.T) {
	srv := newServer(t, http.StatusBadRequest, `{"error_messages":["transaction_details.order_id has already been taken"]}`)
	p := newProvider(t, srv.URL, "http://unused.invalid")

	_, err := p.CreatePayment(context.Background(), paymentRequest())

	var gw *payment.GatewayError
	require.ErrorAs(t, err, &gw)
	assert.Equal(t, http.StatusBadRequest, gw.StatusCode)
	assert.Contains(t, err.Error(), "order_id has already been taken")
	assert.NotContains(t, err.Error(), serverKey)
}

func TestCreatePayment_AnOutageIsAGatewayError(t *testing.T) {
	srv := newServer(t, http.StatusBadGateway, `<html>bad gateway</html>`)
	p := newProvider(t, srv.URL, "http://unused.invalid")

	_, err := p.CreatePayment(context.Background(), paymentRequest())

	var gw *payment.GatewayError
	require.ErrorAs(t, err, &gw)
	assert.Equal(t, http.StatusBadGateway, gw.StatusCode)
}

func TestCreatePayment_AnUnreachableGatewayIsAGatewayError(t *testing.T) {
	srv := newServer(t, http.StatusCreated, `{}`)
	url := srv.URL
	srv.Close()
	p := newProvider(t, url, "http://unused.invalid")

	_, err := p.CreatePayment(context.Background(), paymentRequest())

	var gw *payment.GatewayError
	require.ErrorAs(t, err, &gw)
	assert.NotContains(t, err.Error(), serverKey)
}

func TestCreatePayment_ASuccessWithoutAnURLIsAnError(t *testing.T) {
	srv := newServer(t, http.StatusCreated, `{"token":"t"}`)
	p := newProvider(t, srv.URL, "http://unused.invalid")

	_, err := p.CreatePayment(context.Background(), paymentRequest())

	require.Error(t, err)
}

func TestCreatePayment_HonoursTheContextDeadline(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(block); srv.Close() })
	p := newProvider(t, srv.URL, "http://unused.invalid")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := p.CreatePayment(ctx, paymentRequest())

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded), err.Error())
}

func TestCreatePayment_OnlyRupiahAndPositiveAmounts(t *testing.T) {
	p := newProvider(t, "http://unused.invalid", "http://unused.invalid")
	usd := paymentRequest()
	usd.Currency = "USD"
	free := paymentRequest()
	free.Amount = 0

	_, err := p.CreatePayment(context.Background(), usd)
	require.Error(t, err)
	_, err = p.CreatePayment(context.Background(), free)
	require.Error(t, err)
}

func TestGetPaymentStatus_ReadsTheStatusAPI(t *testing.T) {
	srv := newServer(t, http.StatusOK, `{"status_code":"200","transaction_id":"txn-1","transaction_status":"settlement","order_id":"INV-2026-000001",`+
		`"gross_amount":"99000.00","currency":"IDR","fraud_status":"accept","signature_key":"`+sigInvoice1Paid+`"}`)
	p := newProvider(t, "http://unused.invalid", srv.URL)

	st, err := p.GetPaymentStatus(context.Background(), "INV-2026-000001")

	require.NoError(t, err)
	assert.Equal(t, payment.StatePaid, st.State)
	assert.Equal(t, int64(99000), st.Amount)
	assert.Equal(t, "IDR", st.Currency)
	assert.Equal(t, "INV-2026-000001", st.InvoiceNumber)
	assert.Equal(t, "txn-1", st.ProviderRef)
	call := srv.last()
	assert.Equal(t, http.MethodGet, call.method)
	assert.Equal(t, "/v2/INV-2026-000001/status", call.path)
	assert.Equal(t, basicAuth, call.auth)
}

func TestGetPaymentStatus_AnOrderMidtransDoesNotKnowIsNotFound(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"200 with status_code 404": {http.StatusOK, `{"status_code":"404","status_message":"Transaction doesn't exist."}`},
		"http 404":                 {http.StatusNotFound, `{"status_code":"404"}`},
	} {
		t.Run(name, func(t *testing.T) {
			srv := newServer(t, tc.status, tc.body)
			p := newProvider(t, "http://unused.invalid", srv.URL)

			_, err := p.GetPaymentStatus(context.Background(), "INV-X")

			assert.ErrorIs(t, err, payment.ErrNotFound)
		})
	}
}

func TestGetPaymentStatus_ASignedAnswerWithAWrongSignatureIsRefused(t *testing.T) {
	srv := newServer(t, http.StatusOK, `{"status_code":"200","transaction_id":"txn-1","transaction_status":"settlement","order_id":"INV-2026-000001",`+
		`"gross_amount":"99000.00","signature_key":"`+sigInvoice2Expired+`"}`)
	p := newProvider(t, "http://unused.invalid", srv.URL)

	_, err := p.GetPaymentStatus(context.Background(), "INV-2026-000001")

	var gw *payment.GatewayError
	require.ErrorAs(t, err, &gw)
}

func TestGetPaymentStatus_EscapesTheReference(t *testing.T) {
	srv := newServer(t, http.StatusNotFound, `{}`)
	p := newProvider(t, "http://unused.invalid", srv.URL)

	_, err := p.GetPaymentStatus(context.Background(), "../../secret")

	assert.ErrorIs(t, err, payment.ErrNotFound)
	assert.NotEqual(t, "/secret/status", srv.last().path, "a reference cannot climb out of /v2/")
}

// sha512Hex is only for ad-hoc cases; the main vectors above are hard-coded.
func sha512Hex(s string) string {
	sum := sha512.Sum512([]byte(s))
	return hex.EncodeToString(sum[:])
}
