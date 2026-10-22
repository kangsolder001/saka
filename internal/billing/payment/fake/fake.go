// Package fake is a payment.Provider for development and tests: no network,
// deterministic payment links, a signed-webhook scheme of its own and states
// the test controls. It is refused by the configuration in production.
package fake

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/kangsolder001/saka/internal/billing/payment"
)

// Name is the provider name: the {provider} of the webhook route.
const Name = "fake"

// SignatureHeader carries the signature of a webhook body.
const SignatureHeader = "X-Fake-Signature"

// secret signs the fake's webhooks. It is public on purpose: the fake only
// exists outside production, and tests must be able to forge notifications.
const secret = "fake-webhook-secret" //nolint:gosec // public on purpose, see above

// PaymentURL is the link CreatePayment returns for an invoice.
func PaymentURL(invoiceNumber string) string { return "https://pay.fake.test/" + invoiceNumber }

// Sign returns the value of SignatureHeader for body.
func Sign(body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// wire is the body of a fake webhook.
type wire struct {
	EventID       string        `json:"event_id"`
	InvoiceNumber string        `json:"invoice_number"`
	ProviderRef   string        `json:"provider_ref"`
	State         payment.State `json:"state"`
	Amount        int64         `json:"amount"`
	Currency      string        `json:"currency"`
}

// Webhook builds the body and the headers of a notification the fake accepts.
func Webhook(ev payment.WebhookEvent) ([]byte, http.Header) {
	body, err := json.Marshal(wire{
		EventID: ev.EventID, InvoiceNumber: ev.InvoiceNumber, ProviderRef: ev.ProviderRef,
		State: ev.State, Amount: ev.Amount, Currency: ev.Currency,
	})
	if err != nil {
		panic(err) // a struct of plain fields always marshals
	}
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set(SignatureHeader, Sign(body))
	return body, h
}

// Provider is the fake gateway. It is safe for concurrent use.
type Provider struct {
	mu      sync.Mutex
	created []payment.PaymentRequest
	states  map[string]payment.State
	failing error
}

var _ payment.Provider = (*Provider)(nil)

// New creates a fake gateway with no payments.
func New() *Provider {
	return &Provider{states: map[string]payment.State{}}
}

// Name implements payment.Provider.
func (p *Provider) Name() string { return Name }

// CreatePayment records the request and returns a link derived from the
// invoice number. Creating the same invoice number twice is refused, like a
// real gateway refuses a reused order id.
func (p *Provider) CreatePayment(_ context.Context, req payment.PaymentRequest) (payment.PaymentSession, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failing != nil {
		return payment.PaymentSession{}, &payment.GatewayError{Op: "create payment", StatusCode: http.StatusBadGateway, Message: "simulated failure", Err: p.failing}
	}
	for _, c := range p.created {
		if c.InvoiceNumber == req.InvoiceNumber {
			return payment.PaymentSession{}, &payment.GatewayError{Op: "create payment", StatusCode: http.StatusBadRequest, Message: "order id already taken"}
		}
	}
	p.created = append(p.created, req)
	p.states[req.InvoiceNumber] = payment.StatePending
	return payment.PaymentSession{Ref: req.InvoiceNumber, URL: PaymentURL(req.InvoiceNumber)}, nil
}

// VerifyWebhook checks SignatureHeader (HMAC-SHA256 of the body) and decodes
// the body written by Webhook.
func (p *Provider) VerifyWebhook(r *http.Request) (payment.WebhookEvent, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return payment.WebhookEvent{}, fmt.Errorf("%w: %w", payment.ErrMalformedEvent, err)
	}
	got, err := hex.DecodeString(r.Header.Get(SignatureHeader))
	want, _ := hex.DecodeString(Sign(raw))
	if err != nil || !hmac.Equal(got, want) {
		return payment.WebhookEvent{}, payment.ErrInvalidSignature
	}
	var w wire
	if err := json.Unmarshal(raw, &w); err != nil || w.EventID == "" || w.InvoiceNumber == "" || w.State == "" {
		return payment.WebhookEvent{}, fmt.Errorf("%w: event_id, invoice_number and state are required", payment.ErrMalformedEvent)
	}
	return payment.WebhookEvent{
		EventID: w.EventID, InvoiceNumber: w.InvoiceNumber, ProviderRef: w.ProviderRef, State: w.State,
		Amount: w.Amount, Currency: w.Currency, Raw: bytes.Clone(raw),
	}, nil
}

// GetPaymentStatus reports the state the test set with SetState (pending
// until then); an invoice that was never created is payment.ErrNotFound.
func (p *Provider) GetPaymentStatus(_ context.Context, ref string) (payment.PaymentStatus, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failing != nil {
		return payment.PaymentStatus{}, &payment.GatewayError{Op: "get status", StatusCode: http.StatusBadGateway, Message: "simulated failure", Err: p.failing}
	}
	for _, c := range p.created {
		if c.InvoiceNumber == ref {
			raw, _ := json.Marshal(map[string]any{"invoice_number": ref, "state": p.states[ref]})
			return payment.PaymentStatus{
				InvoiceNumber: ref, ProviderRef: ref, State: p.states[ref], Amount: c.Amount, Currency: c.Currency, Raw: raw,
			}, nil
		}
	}
	return payment.PaymentStatus{}, payment.ErrNotFound
}

// SetState changes what GetPaymentStatus reports for an invoice (as if the
// customer paid, or the payment expired, on the gateway).
func (p *Provider) SetState(invoiceNumber string, state payment.State) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.states[invoiceNumber] = state
}

// FailWith makes every following call fail like an unreachable gateway; nil
// restores normal behaviour.
func (p *Provider) FailWith(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failing = err
}

// Created returns the payment requests received so far, oldest first.
func (p *Provider) Created() []payment.PaymentRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]payment.PaymentRequest(nil), p.created...)
}

// Last returns the most recent payment request.
func (p *Provider) Last() (payment.PaymentRequest, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.created) == 0 {
		return payment.PaymentRequest{}, errors.New("fake: no payment was created")
	}
	return p.created[len(p.created)-1], nil
}
