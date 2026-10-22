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
