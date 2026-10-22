// Package midtrans is the Midtrans (Snap) implementation of payment.Provider.
//
// It is written against Midtrans' public documentation and tested with a fake
// HTTP server and hand-computed signatures; it has NOT been run against the
// real sandbox yet (the account is still to be created; see docs/spec.md §8.7
// for the assumptions to verify when it exists).
//
// Flow: CreatePayment calls the Snap API (POST /snap/v1/transactions) with the
// invoice number as order_id and returns the redirect_url. Midtrans then posts
// HTTP notifications to /api/v1/webhooks/midtrans, signed with
// SHA512(order_id + status_code + gross_amount + ServerKey). The payment state
// is only ever taken from a verified notification or from the status API
// (GET /v2/{order_id}/status), never from the customer's redirect.
package midtrans

import (
	"bytes"
	"context"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kangsolder001/saka/internal/billing/payment"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

// Name is the provider name: the {provider} of the webhook route.
const Name = "midtrans"

// Sandbox hosts; production uses app.midtrans.com and api.midtrans.com.
const (
	SandboxSnapBaseURL = "https://app.sandbox.midtrans.com"
	SandboxAPIBaseURL  = "https://api.sandbox.midtrans.com"
)

const (
	maxResponseBytes = 1 << 20
	maxWebhookBytes  = 1 << 20
	maxErrorMessage  = 200
)

// Config configures the provider. ServerKey is a secret: it is only used to
// authenticate calls and to verify signatures, and never logged or put in an
// error message.
type Config struct {
	ServerKey   string
	SnapBaseURL string
	APIBaseURL  string
	// Timeout bounds every call to Midtrans.
	Timeout time.Duration
	// HTTPClient replaces the default client (tests).
	HTTPClient *http.Client
}

// Provider implements payment.Provider for Midtrans.
type Provider struct {
	serverKey string
	snapURL   string
	apiURL    string
	client    *http.Client
	clk       clock.Clock
}

var _ payment.Provider = (*Provider)(nil)

// New creates the provider. The clock decides the start of payment expiry.
func New(cfg Config, clk clock.Clock) (*Provider, error) {
	if cfg.ServerKey == "" {
		return nil, errors.New("midtrans: server key is required")
	}
	for name, raw := range map[string]string{"snap": cfg.SnapBaseURL, "api": cfg.APIBaseURL} {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("midtrans: %s base url must be an http(s) URL", name)
		}
	}
	client := cfg.HTTPClient
	if client == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = 15 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}
	return &Provider{
		serverKey: cfg.ServerKey,
		snapURL:   strings.TrimRight(cfg.SnapBaseURL, "/"),
		apiURL:    strings.TrimRight(cfg.APIBaseURL, "/"),
		client:    client,
		clk:       clk,
	}, nil
}

// Name implements payment.Provider.
func (p *Provider) Name() string { return Name }

type snapRequest struct {
	TransactionDetails transactionDetails `json:"transaction_details"`
	CustomerDetails    customerDetails    `json:"customer_details"`
	ItemDetails        []itemDetails      `json:"item_details"`
	Expiry             expiry             `json:"expiry"`
}

type transactionDetails struct {
	OrderID     string `json:"order_id"`
	GrossAmount int64  `json:"gross_amount"`
}

type customerDetails struct {
	FirstName string `json:"first_name,omitempty"`
	Email     string `json:"email"`
}

type itemDetails struct {
	ID       string `json:"id"`
	Price    int64  `json:"price"`
	Quantity int    `json:"quantity"`
	Name     string `json:"name"`
}

type expiry struct {
	StartTime string `json:"start_time"`
	Unit      string `json:"unit"`
	Duration  int64  `json:"duration"`
}

type snapResponse struct {
	Token         string   `json:"token"`
	RedirectURL   string   `json:"redirect_url"`
	ErrorMessages []string `json:"error_messages"`
}

// CreatePayment creates a Snap transaction for the invoice: order_id is the
// invoice number, gross_amount the invoice amount (IDR has no decimals), and
// the link stops working at req.ExpiresAt. The returned Ref is the order id,
// which the status API accepts.
func (p *Provider) CreatePayment(ctx context.Context, req payment.PaymentRequest) (payment.PaymentSession, error) {
	if req.Currency != "IDR" {
		return payment.PaymentSession{}, &payment.GatewayError{Op: "create payment", Message: "only IDR is supported"}
	}
	if req.InvoiceNumber == "" || req.Amount <= 0 {
		return payment.PaymentSession{}, &payment.GatewayError{Op: "create payment", Message: "invoice number and a positive amount are required"}
	}
	now := p.clk.Now()
	minutes := int64(req.ExpiresAt.Sub(now) / time.Minute)
	if req.ExpiresAt.Sub(now)%time.Minute != 0 {
		minutes++
	}
	if minutes < 1 {
		minutes = 1
	}
	body := snapRequest{
		TransactionDetails: transactionDetails{OrderID: req.InvoiceNumber, GrossAmount: req.Amount},
		CustomerDetails:    customerDetails{FirstName: truncate(req.Customer.Name, 255), Email: req.Customer.Email},
		ItemDetails: []itemDetails{{
			ID: req.InvoiceNumber, Price: req.Amount, Quantity: 1, Name: truncate(req.Description, 50),
		}},
		Expiry: expiry{StartTime: now.Format("2006-01-02 15:04:05 -0700"), Unit: "minutes", Duration: minutes},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return payment.PaymentSession{}, fmt.Errorf("midtrans: marshal request: %w", err)
	}

	respBody, status, err := p.do(ctx, http.MethodPost, p.snapURL+"/snap/v1/transactions", bytes.NewReader(payload), "create payment")
	if err != nil {
		return payment.PaymentSession{}, err
	}
	var resp snapResponse
	decodeErr := json.Unmarshal(respBody, &resp)
	if status < 200 || status > 299 {
		// The error body is best effort: a gateway outage may answer with HTML.
		return payment.PaymentSession{}, &payment.GatewayError{Op: "create payment", StatusCode: status, Message: firstMessage(resp.ErrorMessages)}
	}
	if decodeErr != nil {
		return payment.PaymentSession{}, &payment.GatewayError{Op: "create payment", StatusCode: status, Message: "unreadable response", Err: decodeErr}
	}
	if resp.RedirectURL == "" {
		return payment.PaymentSession{}, &payment.GatewayError{Op: "create payment", StatusCode: status, Message: "response has no redirect_url"}
	}
	return payment.PaymentSession{Ref: req.InvoiceNumber, URL: resp.RedirectURL}, nil
}

// GetPaymentStatus asks Midtrans for the state of an order (or transaction)
// id. A payment the customer never started does not exist for Midtrans and
// yields payment.ErrNotFound.
func (p *Provider) GetPaymentStatus(ctx context.Context, ref string) (payment.PaymentStatus, error) {
	if ref == "" {
		return payment.PaymentStatus{}, &payment.GatewayError{Op: "get status", Message: "reference is required"}
	}
	respBody, status, err := p.do(ctx, http.MethodGet, p.apiURL+"/v2/"+url.PathEscape(ref)+"/status", nil, "get status")
	if err != nil {
		return payment.PaymentStatus{}, err
	}
	if status == http.StatusNotFound {
		return payment.PaymentStatus{}, payment.ErrNotFound
	}
	if status < 200 || status > 299 {
		return payment.PaymentStatus{}, &payment.GatewayError{Op: "get status", StatusCode: status}
	}
	var n notification
	if err := json.Unmarshal(respBody, &n); err != nil {
		return payment.PaymentStatus{}, &payment.GatewayError{Op: "get status", StatusCode: status, Message: "unreadable response", Err: err}
	}
	// Midtrans answers 200 with status_code "404" in the body for an unknown order.
	if n.StatusCode.String() == "404" {
		return payment.PaymentStatus{}, payment.ErrNotFound
	}
	// The status API signs its answer like a notification; check it when it does.
	if n.SignatureKey != "" && !p.validSignature(n) {
		return payment.PaymentStatus{}, &payment.GatewayError{Op: "get status", StatusCode: status, Message: "response signature mismatch"}
	}
	ev, err := p.normalize(n, respBody)
	if err != nil {
		return payment.PaymentStatus{}, &payment.GatewayError{Op: "get status", StatusCode: status, Message: "unreadable response", Err: err}
	}
	return payment.PaymentStatus{
		InvoiceNumber: ev.InvoiceNumber, ProviderRef: ev.ProviderRef, State: ev.State,
		Amount: ev.Amount, Currency: ev.Currency, Raw: ev.Raw,
	}, nil
}

// VerifyWebhook authenticates a Midtrans HTTP notification: the body must carry
// signature_key = hex(SHA512(order_id + status_code + gross_amount + ServerKey)),
// compared in constant time, before anything in it is believed.
func (p *Provider) VerifyWebhook(r *http.Request) (payment.WebhookEvent, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBytes+1))
	if err != nil {
		return payment.WebhookEvent{}, fmt.Errorf("%w: read body: %w", payment.ErrMalformedEvent, err)
	}
	if len(raw) > maxWebhookBytes {
		return payment.WebhookEvent{}, fmt.Errorf("%w: body too large", payment.ErrMalformedEvent)
	}
	var n notification
	if err := json.Unmarshal(raw, &n); err != nil {
		return payment.WebhookEvent{}, fmt.Errorf("%w: %w", payment.ErrMalformedEvent, err)
	}
	if n.SignatureKey == "" || !p.validSignature(n) {
		return payment.WebhookEvent{}, payment.ErrInvalidSignature
	}
	return p.normalize(n, raw)
}

// notification is the body of a Midtrans notification and of the status API.
// Midtrans sends strings, but amounts and codes are read leniently (a JSON
// number keeps its text, which is what the signature is computed over).
type notification struct {
	TransactionID     string     `json:"transaction_id"`
	TransactionStatus string     `json:"transaction_status"`
	StatusCode        flexString `json:"status_code"`
	OrderID           string     `json:"order_id"`
	GrossAmount       flexString `json:"gross_amount"`
	FraudStatus       string     `json:"fraud_status"`
	SignatureKey      string     `json:"signature_key"`
	Currency          string     `json:"currency"`
}

// flexString decodes a JSON string or number into its text.
type flexString string

func (f *flexString) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	switch {
	case len(data) > 0 && data[0] == '"':
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*f = flexString(s)
	case bytes.Equal(data, []byte("null")):
		*f = ""
	default:
		*f = flexString(data)
	}
	return nil
}

func (f flexString) String() string { return string(f) }

func (p *Provider) validSignature(n notification) bool {
	sum := sha512.Sum512([]byte(n.OrderID + n.StatusCode.String() + n.GrossAmount.String() + p.serverKey))
	want := hex.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(strings.ToLower(n.SignatureKey)), []byte(want)) == 1
}

// normalize turns a notification (already authenticated) into the common form.
func (p *Provider) normalize(n notification, raw []byte) (payment.WebhookEvent, error) {
	if n.OrderID == "" || n.TransactionStatus == "" {
		return payment.WebhookEvent{}, fmt.Errorf("%w: order_id and transaction_status are required", payment.ErrMalformedEvent)
	}
	amount, err := parseRupiah(n.GrossAmount.String())
	if err != nil {
		return payment.WebhookEvent{}, fmt.Errorf("%w: gross_amount: %w", payment.ErrMalformedEvent, err)
	}
	currency := n.Currency
	if currency == "" {
		currency = "IDR" // Midtrans only settles in rupiah
	}
	return payment.WebhookEvent{
		EventID:       eventID(n),
		InvoiceNumber: n.OrderID,
		ProviderRef:   n.TransactionID,
		State:         stateOf(n.TransactionStatus, n.FraudStatus),
		Amount:        amount,
		Currency:      currency,
		Raw:           append([]byte(nil), raw...),
	}, nil
}

// eventID derives a stable ID, because Midtrans notifications carry none: a
// notification is identified by the transaction, what happened to it and how
// the fraud check ended. Midtrans re-sends the same notification when it gets
// no 2xx, and every such delivery yields the same ID. (A state reached twice
// by one transaction, such as two partial refunds, collapses into one event;
// billing takes no action on refunds anyway beyond flagging them.) Without a
// transaction id (a test notification) the order id stands in.
func eventID(n notification) string {
	txn := n.TransactionID
	if txn == "" {
		txn = n.OrderID
	}
	return strings.Join([]string{txn, n.TransactionStatus, n.StatusCode.String(), n.FraudStatus}, ":")
}

// stateOf maps transaction_status (and fraud_status for cards) to a State. An
// unknown status is treated as pending: nothing is activated on a status that
// is not understood, and the payload is kept in payment_events.
func stateOf(transactionStatus, fraudStatus string) payment.State {
	switch transactionStatus {
	case "settlement":
		return payment.StatePaid
	case "capture":
		// A card payment captured but held for review is not money yet.
		if fraudStatus == "" || fraudStatus == "accept" {
			return payment.StatePaid
		}
		if fraudStatus == "deny" {
			return payment.StateFailed
		}
		return payment.StatePending
	case "deny", "failure":
		return payment.StateFailed
	case "cancel":
		return payment.StateCanceled
	case "expire":
		return payment.StateExpired
	case "refund", "partial_refund", "chargeback", "partial_chargeback":
		return payment.StateRefunded
	default: // pending, authorize and anything new
		return payment.StatePending
	}
}

// parseRupiah reads Midtrans' gross_amount ("99000.00") as whole rupiah. A
// fraction other than zeros is not rupiah and is refused rather than rounded:
// money is never guessed.
func parseRupiah(s string) (int64, error) {
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" || strings.Trim(frac, "0") != "" {
		return 0, fmt.Errorf("%q is not a whole amount", s)
	}
	v, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("%q is not a valid amount", s)
	}
	return v, nil
}

// do performs an authenticated call and returns the (size-limited) body and
// the HTTP status. The trace ID of ctx travels as a traceparent header.
func (p *Provider) do(ctx context.Context, method, target string, body io.Reader, op string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, 0, &payment.GatewayError{Op: op, Message: "build request", Err: err}
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(p.serverKey+":")))
	if tp := reqctx.Traceparent(ctx); tp != "" {
		req.Header.Set("traceparent", tp)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		// url.Error repeats the URL, which has no secrets (the key is in a header).
		return nil, 0, &payment.GatewayError{Op: op, Message: "request failed", Err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, resp.StatusCode, &payment.GatewayError{Op: op, StatusCode: resp.StatusCode, Message: "read response", Err: err}
	}
	return data, resp.StatusCode, nil
}

func firstMessage(msgs []string) string {
	if len(msgs) == 0 {
		return ""
	}
	return truncate(msgs[0], maxErrorMessage)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
