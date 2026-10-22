package org

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/org/orgdb"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

// TransferOwnership hands the current organization to another member (spec
// §7.5). Only the owner may, and must confirm their password. In one
// transaction the target becomes owner, the previous owner becomes admin and
// organizations.owner_id follows, so there is never a moment with two owners,
// none, or an owner_id that disagrees with the memberships.
//
// Both memberships are row-locked and the owner is re-read under the lock, so
// two transfers racing each other cannot both succeed: the loser finds it is no
// longer the owner.
func (s *Service) TransferOwnership(ctx context.Context, targetID uuid.UUID, currentPassword string) error {
	if err := Authorize(ctx, ActionTransferOwnership); err != nil {
		return err
	}
	orgID, err := tenant(ctx)
	if err != nil {
		return err
	}
	ownerID, ok := reqctx.UserID(ctx)
	if !ok {
		return apperr.Unauthenticated()
	}
	if targetID == ownerID {
		return apperr.Validation(map[string]string{"user_id": "must be another member"})
	}
	// The password is checked before anything is locked or changed, and a wrong
	// one counts against the same per-user limit as every other sensitive action.
	if err := s.Passwords.ConfirmPassword(ctx, ownerID, currentPassword); err != nil {
		return err
	}

	return s.tx.WithTx(ctx, func(tx pgx.Tx) error {
		q := orgdb.New(tx)
		// Lock in a fixed order (by user ID) so concurrent transfers cannot deadlock.
		first, second := ownerID, targetID
		if second.String() < first.String() {
			first, second = second, first
		}
		for _, id := range []uuid.UUID{first, second} {
			if _, err := q.GetMembershipForUpdate(ctx, orgdb.GetMembershipForUpdateParams{OrganizationID: orgID, UserID: id}); err != nil {
				if isNoRows(err) {
					if id == targetID {
						return apperr.NotFound()
					}
					return apperr.Forbidden()
				}
				return fmt.Errorf("org: lock membership: %w", err)
			}
		}

		// Re-checked under the lock: the request-start role may be stale.
		demoted, err := q.DemoteOwnerToAdmin(ctx, orgdb.DemoteOwnerToAdminParams{OrganizationID: orgID, UserID: ownerID})
		if err != nil {
			return fmt.Errorf("org: demote owner: %w", err)
		}
		if demoted == 0 {
			return apperr.Forbidden()
		}
		promoted, err := q.PromoteToOwner(ctx, orgdb.PromoteToOwnerParams{OrganizationID: orgID, UserID: targetID})
		if err != nil {
			return fmt.Errorf("org: promote owner: %w", err)
		}
		if promoted == 0 {
			return apperr.NotFound()
		}
		changed, err := q.SetOrganizationOwner(ctx, orgdb.SetOrganizationOwnerParams{
			ID: orgID, OldOwnerID: ownerID, NewOwnerID: targetID, Now: s.clk.Now(),
		})
		if err != nil {
			return fmt.Errorf("org: set owner: %w", err)
		}
		if changed == 0 {
			return apperr.Forbidden()
		}
		return nil
	})
}
