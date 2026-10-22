package billing

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/billing/billingdb"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/db"
)

// StartFree gives a new organization its Free subscription: active, with a
// period that never ends. It runs in the caller's transaction (the one that
// creates the organization), which must already be pinned to the new tenant
// for row-level security. An organization has exactly one subscription, so
// calling it twice is an error.
func (s *Service) StartFree(ctx context.Context, q db.DBTX, orgID uuid.UUID) error {
	_, err := queries(q).InsertFreeSubscription(ctx, billingdb.InsertFreeSubscriptionParams{
		OrganizationID: orgID, Now: s.clk.Now(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("billing: start free subscription: no %q plan in the catalogue", FreePlanCode)
	}
	if err != nil {
		return fmt.Errorf("billing: start free subscription: %w", err)
	}
	return nil
}

// GetSubscription returns the organization's subscription, or NOT_FOUND when it
// has none (every organization gets one when it is created).
func (s *Service) GetSubscription(ctx context.Context, q db.DBTX, orgID uuid.UUID) (Subscription, error) {
	row, err := queries(q).GetSubscriptionByOrganization(ctx, orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Subscription{}, apperr.NotFound()
	}
	if err != nil {
		return Subscription{}, fmt.Errorf("billing: get subscription: %w", err)
	}
	return subscriptionFromRow(row), nil
}

// OrganizationDeleted ends the subscription of a deleted organization: it is
// canceled (spec §6, §8.2) and every invoice still waiting for payment is
// voided, so nothing keeps billing or renewing an organization that no longer
// exists. It runs in the transaction that deletes the organization (q is
// pinned to it, or a bypass one on the account-deletion path) and is safe to
// call again. The gateway link of a voided invoice cannot be revoked through
// the PaymentProvider contract; a payment made through it later is recorded
// and flagged for a person (spec §8.7), never activated. Invoices already paid
// are history and stay as they are.
func (s *Service) OrganizationDeleted(ctx context.Context, q db.DBTX, orgID uuid.UUID) error {
	sub, err := queries(q).LockSubscriptionByOrganization(ctx, orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // an organization without a subscription has nothing to end
	}
	if err != nil {
		return fmt.Errorf("billing: lock subscription of deleted organization: %w", err)
	}
	if _, err := s.transition(ctx, q, sub, Event{Kind: EventOrganizationDeleted}); err != nil && !errors.Is(err, ErrIllegalTransition) {
		return fmt.Errorf("billing: cancel subscription of deleted organization: %w", err)
	}
	if _, err := queries(q).VoidLiveInvoices(ctx, billingdb.VoidLiveInvoicesParams{SubscriptionID: sub.ID, Now: s.clk.Now()}); err != nil {
		return fmt.Errorf("billing: void invoices of deleted organization: %w", err)
	}
	return nil
}
