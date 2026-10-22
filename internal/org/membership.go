package org

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

// Membership returns userID's membership in orgID. A user who is not a member,
// an unknown organization and a deleted one all return NOT_FOUND (spec §7.3:
// the answer must not reveal which organizations exist).
//
// The lookup runs in a transaction pinned to orgID, so row-level security
// scopes it to that tenant. orgID comes from the URL here, which is safe: the
// query still requires the caller's own membership row.
func (s *Service) Membership(ctx context.Context, orgID, userID uuid.UUID) (Membership, error) {
	var m Membership
	err := s.tx.WithTx(reqctx.WithOrgID(ctx, orgID), func(tx pgx.Tx) error {
		row, err := orgdbQueries(tx).GetMembership(ctx, getMembershipParams(orgID, userID))
		if err != nil {
			if isNoRows(err) {
				return apperr.NotFound()
			}
			return fmt.Errorf("org: get membership: %w", err)
		}
		m = membershipFromRow(row)
		return nil
	})
	return m, err
}
