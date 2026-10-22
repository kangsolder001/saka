package org

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/kangsolder001/saka/internal/auth"
	"github.com/kangsolder001/saka/internal/org/orgdb"
	"github.com/kangsolder001/saka/internal/platform/db"
)

var _ auth.OrganizationGuard = (*Service)(nil)

// BeforeUserDeleted is the organization module's part of account deletion
// (spec §6); auth calls it inside the deletion transaction.
//
// A user who owns an organization that still has other members is refused with
// auth.ErrOwnsSharedOrganization: they must transfer ownership first. For
// everything else the user is taken out: memberships in organizations they
// merely belong to are removed, and organizations where they are the only
// member are soft-deleted (with their open invitations revoked). The owner's
// own membership row of a deleted organization is kept, because the
// organization's owner foreign key points at it.
//
// Each organization row is locked exclusively before its members are counted.
// Joining (invitation accept, email verification) share-locks the same row, so
// nobody can slip in between the count and the cleanup, and a join that loses
// the race finds the organization deleted.
func (s *Service) BeforeUserDeleted(ctx context.Context, q db.DBTX, userID uuid.UUID) error {
	return db.WithBypass(ctx, q, func() error {
		queries := orgdbQueries(q)
		memberships, err := queries.ListUserMemberships(ctx, userID)
		if err != nil {
			return fmt.Errorf("org: list memberships of deleted user: %w", err)
		}
		for _, m := range memberships {
			// Lock order is by organization ID (the query's ORDER BY), so two users
			// being deleted from the same organizations cannot deadlock.
			if _, err := queries.LockOrganizationForUpdate(ctx, m.OrganizationID); err != nil {
				if isNoRows(err) {
					continue // deleted since the listing
				}
				return fmt.Errorf("org: lock organization: %w", err)
			}
			others, err := queries.CountOtherMembers(ctx, orgdb.CountOtherMembersParams{OrganizationID: m.OrganizationID, UserID: userID})
			if err != nil {
				return fmt.Errorf("org: count members: %w", err)
			}
			switch {
			case Role(m.Role) == RoleOwner && others > 0:
				return auth.ErrOwnsSharedOrganization()
			case Role(m.Role) == RoleOwner:
				if err := s.deleteOrganization(ctx, q, m.OrganizationID); err != nil {
					return err
				}
			default:
				if _, err := queries.DeleteMembership(ctx, orgdb.DeleteMembershipParams{OrganizationID: m.OrganizationID, UserID: userID}); err != nil {
					return fmt.Errorf("org: remove deleted user: %w", err)
				}
			}
		}
		return nil
	})
}
