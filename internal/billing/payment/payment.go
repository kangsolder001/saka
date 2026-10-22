// Package payment defines the contract between billing and a payment gateway
// (spec §8.1) and the types that cross it. Gateways live in subpackages
// (midtrans, fake) and are chosen by configuration; billing only ever sees a
// Provider.
package payment

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Provider is a payment gateway. Billing keeps its own subscription engine
// (spec §8.1): the gateway only creates payments and reports their outcome.
type Provider interface {
	// Name identifies the gateway: the {provider} of /webhooks/{provider} and
	// invoices.provider ("midtrans", "fake").
	Name() string

	// CreatePayment creates a payment the customer can pay and returns the
	// link to it. The invoice number is the reference the gateway reports back.
	CreatePayment(ctx context.Context, req PaymentRequest) (PaymentSession, error)

	// VerifyWebhook authenticates a notification and normalizes it. It must
	// check the signature before anything else and return ErrInvalidSignature
	// when it does not match; a body that cannot be understood is
	// ErrMalformedEvent. It reads r.Body (once) and returns the raw bytes in
	// WebhookEvent.Raw, which must be valid JSON.
	VerifyWebhook(r *http.Request) (WebhookEvent, error)

	// GetPaymentStatus asks the gateway for the current state of a payment by
	// the reference of the invoice (its number, or the gateway's transaction
	// id once known). ErrNotFound means the gateway has no such payment (for
	// instance because the customer never opened it).
	GetPaymentStatus(ctx context.Context, ref string) (PaymentStatus, error)
}

// Customer is who pays.
type Customer struct {
	Name  string
	Email string
}

// PaymentRequest is what billing asks a gateway to collect. Amount and
// currency come from the invoice (never from the client).
type PaymentRequest struct {
	// InvoiceNumber is the order reference (unique, never reused).
	InvoiceNumber string
	// Amount is in the smallest unit of Currency (IDR: rupiah).
	Amount      int64
	Currency    string
	Description string
	Customer    Customer
	// ExpiresAt is when the payment link stops working.
	ExpiresAt time.Time
}

// PaymentSession is the payment a gateway created.
type PaymentSession struct {
	// Ref is how billing asks about the payment later (GetPaymentStatus).
	Ref string
	// URL is where the customer pays.
	URL string
}

// State is the outcome of a payment, normalized across gateways.
type State string

const (
	// StatePending: created or started, not paid yet. Also a payment held for
	// a fraud review.
	StatePending State = "pending"
	// StatePaid: the money is settled.
	StatePaid State = "paid"
	// StateFailed: an attempt was refused; the customer may try again.
	StateFailed State = "failed"
	// StateExpired: the payment window closed without a payment.
	StateExpired State = "expired"
	// StateCanceled: the payment was canceled on the gateway.
	StateCanceled State = "canceled"
	// StateRefunded: money was returned (refund or chargeback).
	StateRefunded State = "refunded"
)

// WebhookEvent is a notification from the gateway, after verification.
type WebhookEvent struct {
	// EventID identifies the notification for idempotency: delivering the same
	// notification twice yields the same EventID.
	EventID string
	// InvoiceNumber is the order reference the payment was created with.
	InvoiceNumber string
	// ProviderRef is the gateway's own id of the payment, when it has one.
	ProviderRef string
	State       State
	// Amount and Currency are what the gateway says was charged; billing
	// checks them against the invoice before trusting a payment.
	Amount   int64
	Currency string
	// Raw is the notification as received, valid JSON.
	Raw []byte
}

// PaymentStatus is the answer of GetPaymentStatus.
type PaymentStatus struct {
	InvoiceNumber string
	ProviderRef   string
	State         State
	Amount        int64
	Currency      string
	// Raw is the gateway's answer, valid JSON.
	Raw []byte
}

// Errors a Provider returns.
var (
	// ErrInvalidSignature: the notification did not come from the gateway.
	ErrInvalidSignature = errors.New("payment: invalid webhook signature")
	// ErrMalformedEvent: the notification is signed or unsigned nonsense that
	// cannot be read.
	ErrMalformedEvent = errors.New("payment: malformed webhook event")
	// ErrNotFound: the gateway has no payment with that reference.
	ErrNotFound = errors.New("payment: payment not found at the gateway")
)

// GatewayError is a failed call to the gateway: it could not be reached, or it
// answered with an error. The message is safe to log (it never carries
// credentials).
type GatewayError struct {
	Op         string
	StatusCode int // HTTP status; 0 when there was no response
	Message    string
	Err        error
}

func (e *GatewayError) Error() string {
	msg := fmt.Sprintf("payment gateway %s", e.Op)
	if e.StatusCode != 0 {
		msg += fmt.Sprintf(": http %d", e.StatusCode)
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *GatewayError) Unwrap() error { return e.Err }

// Registry looks up providers by name.
type Registry map[string]Provider

// NewRegistry builds a Registry; two providers with one name is a wiring bug.
func NewRegistry(providers ...Provider) (Registry, error) {
	r := make(Registry, len(providers))
	for _, p := range providers {
		if _, dup := r[p.Name()]; dup {
			return nil, fmt.Errorf("payment: provider %q registered twice", p.Name())
		}
		r[p.Name()] = p
	}
	return r, nil
}
