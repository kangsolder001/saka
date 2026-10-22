package org

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/kangsolder001/saka/internal/org/orgdb"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/db"
)

// MembersLimit is the name of the plan limit on seats: members plus pending
// invitations (spec §7.4). The composition root registers CountSeats as its
// counter in the entitlement module.
const MembersLimit = "members"

// LimitChecker is what the organization module needs from entitlement
// (*entitlement.Service implements it).
type LimitChecker interface {
	// CheckLimit returns PLAN_LIMIT_REACHED when adding delta to the usage of
	// limit would exceed the organization's plan.
	CheckLimit(ctx context.Context, q db.DBTX, orgID uuid.UUID, limit string, delta int) error
}

// CountSeats returns the seats the organization uses: its members plus its
// live invitations (open and not expired). It counts the real rows every time,
// so it can never drift from them.
func (s *Service) CountSeats(ctx context.Context, q db.DBTX, orgID uuid.UUID) (int, error) {
	n, err := orgdbQueries(q).CountSeats(ctx, orgdb.CountSeatsParams{OrganizationID: orgID, Now: s.clk.Now()})
	if err != nil {
		return 0, fmt.Errorf("org: count seats: %w", err)
	}
	return int(n), nil
}

// reserveSeat checks, inside the caller's transaction, that one more seat fits
// the plan. The caller must have locked the organization (lockOrganization)
// first and inserts the invitation before it commits: the lock is what makes
// concurrent invitations take their turns, since without it two requests could
// both see one free seat and both take it.
func (s *Service) reserveSeat(ctx context.Context, q db.DBTX, orgID uuid.UUID) error {
	if s.Limits == nil {
		return errors.New("org: no limit checker wired")
	}
	return s.Limits.CheckLimit(ctx, q, orgID, MembersLimit, 1)
}

// LockOrganization is the exported form of lockOrganization for modules that
// own a resource with a plan limit: call it first in the transaction that
// checks the limit and creates the resource, so two requests racing for the
// last slot take turns instead of both passing the check (the same way Invite
// serializes seats). q must be the tenant-pinned transaction of the request;
// the lock is released when it ends. A deleted or unknown organization is NOT_FOUND.
func LockOrganization(ctx context.Context, q db.DBTX, orgID uuid.UUID) error {
	return lockOrganization(ctx, q, orgID)
}

// lockOrganization serializes the seat-changing operations of one organization;
// a deleted or unknown organization is NOT_FOUND.
func lockOrganization(ctx context.Context, q db.DBTX, orgID uuid.UUID) error {
	if _, err := orgdbQueries(q).LockOrganizationForUpdate(ctx, orgID); err != nil {
		if isNoRows(err) {
			return apperr.NotFound()
		}
		return fmt.Errorf("org: lock organization: %w", err)
	}
	return nil
}
