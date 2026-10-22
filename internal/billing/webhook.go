package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/billing/billingdb"
	"github.com/kangsolder001/saka/internal/billing/payment"
	"github.com/kangsolder001/saka/internal/platform/apperr"
)

// HandleWebhook handles a payment notification from a gateway (spec §8.5).
//
//  1. The gateway's own code verifies the signature before anything else. A
//     notification that does not verify is UNAUTHENTICATED (401) and nothing is
//     stored; one that cannot be read is a VALIDATION_ERROR (400).
//  2. The verified event is stored in payment_events. A delivery the gateway
//     repeats (they retry whenever they get no 2xx) matches the same
//     (provider, event_id) and is stored once.
//
// A webhook belongs to no tenant until it is matched to an invoice, so it runs
// in system transactions with explicit filters. The status of a payment is
// only ever taken from here or from GetPaymentStatus, never from the browser
// of the customer.
func (s *Service) HandleWebhook(ctx context.Context, providerName string, r *http.Request) error {
	pay, err := s.payments()
	if err != nil {
		return err
	}
	provider, ok := pay.Providers[providerName]
	if !ok {
		return apperr.NotFound()
	}
	ev, err := provider.VerifyWebhook(r)
	switch {
	case errors.Is(err, payment.ErrInvalidSignature):
		zl := pay.Log.FromContext(ctx)
		zl.Warn().Str("provider", providerName).Msg("webhook rejected: invalid signature")
		return apperr.Unauthenticated()
	case errors.Is(err, payment.ErrMalformedEvent):
		return apperr.Validation(map[string]string{"body": "is not a valid notification"})
	case err != nil:
		return fmt.Errorf("billing: verify %s webhook: %w", providerName, err)
	}
	return s.ingest(ctx, pay, provider.Name(), ev)
}

// ingest stores a verified event, once.
func (s *Service) ingest(ctx context.Context, pay *payments, provider string, ev payment.WebhookEvent) error {
	payload := ev.Raw
	if !json.Valid(payload) {
		// The contract says Raw is JSON; never lose an event over a provider's slip.
		payload, _ = json.Marshal(map[string]string{"raw": string(ev.Raw)})
	}
	return pay.Tx.WithSystemTx(ctx, func(tx pgx.Tx) error {
		_, err := queries(tx).InsertPaymentEvent(ctx, billingdb.InsertPaymentEventParams{
			Provider: provider, EventID: ev.EventID, Payload: payload, Now: s.clk.Now(),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // a delivery we have already stored
		}
		if err != nil {
			return fmt.Errorf("billing: store payment event: %w", err)
		}
		return nil
	})
}
