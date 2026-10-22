package org

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/org/orgdb"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/pagination"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

// Member is a user as seen inside an organization.
type Member struct {
	MembershipID uuid.UUID
	UserID       uuid.UUID
	Email        string
	Name         string
	AvatarURL    *string
	Role         Role
	JoinedAt     time.Time
}

// CursorOf is the pagination cursor position of an item in ListMembers.
func (m Member) CursorOf() pagination.Cursor {
	return pagination.Cursor{CreatedAt: m.JoinedAt, ID: m.MembershipID}
}

// ListMembers returns the current organization's members, owner first (oldest
// membership first). It returns up to p.FetchLimit() rows so the caller can
// paginate. Any member may list.
func (s *Service) ListMembers(ctx context.Context, p pagination.Params) ([]Member, error) {
	if err := Authorize(ctx, ActionView); err != nil {
		return nil, err
	}
	orgID, err := tenant(ctx)
	if err != nil {
		return nil, err
	}
	params := orgdb.ListMembershipsParams{OrganizationID: orgID, RowLimit: int32(p.FetchLimit())} //nolint:gosec // bounded by pagination.MaxLimit
	if p.Cursor != nil {
		params.AfterCreatedAt, params.AfterID = &p.Cursor.CreatedAt, &p.Cursor.ID
	}

	var members []Member
	err = s.tx.WithTx(ctx, func(tx pgx.Tx) error {
		rows, err := orgdb.New(tx).ListMemberships(ctx, params)
		if err != nil {
			return fmt.Errorf("org: list memberships: %w", err)
		}
		ids := make([]uuid.UUID, len(rows))
		for i, r := range rows {
			ids[i] = r.UserID
		}
		people, err := s.Users.GetSummaries(ctx, tx, ids)
		if err != nil {
			return err
		}
		for _, r := range rows {
			person, ok := people[r.UserID]
			if !ok {
				continue // a deleted account: its memberships are removed with it, this only hides a race
			}
			members = append(members, Member{
				MembershipID: r.ID, UserID: r.UserID, Email: person.Email, Name: person.Name,
				AvatarURL: person.AvatarURL, Role: Role(r.Role), JoinedAt: r.CreatedAt,
			})
		}
		return nil
	})
	return members, err
}

// memberOf loads one member by user ID inside the tenant of ctx, row-locking
// the membership. NOT_FOUND when the user is not in the organization.
func (s *Service) memberOf(ctx context.Context, tx pgx.Tx, orgID, userID uuid.UUID) (Member, error) {
	row, err := orgdb.New(tx).GetMembershipForUpdate(ctx, orgdb.GetMembershipForUpdateParams{OrganizationID: orgID, UserID: userID})
	if err != nil {
		if isNoRows(err) {
			return Member{}, apperr.NotFound()
		}
		return Member{}, fmt.Errorf("org: get member: %w", err)
	}
	people, err := s.Users.GetSummaries(ctx, tx, []uuid.UUID{userID})
	if err != nil {
		return Member{}, err
	}
	person, ok := people[userID]
	if !ok {
		return Member{}, apperr.NotFound()
	}
	return Member{
		MembershipID: row.ID, UserID: row.UserID, Email: person.Email, Name: person.Name,
		AvatarURL: person.AvatarURL, Role: Role(row.Role), JoinedAt: row.CreatedAt,
	}, nil
}

// ChangeRole sets targetID's role to newRole (spec §7.2: owners manage admins
// and members, admins manage plain members, nobody changes the owner). The
// target is row-locked so concurrent changes serialize.
func (s *Service) ChangeRole(ctx context.Context, targetID uuid.UUID, newRole Role) (Member, error) {
	if err := Authorize(ctx, ActionChangeRole); err != nil {
		return Member{}, err
	}
	orgID, err := tenant(ctx)
	if err != nil {
		return Member{}, err
	}
	actor, _ := RoleFrom(ctx)
	if !Assignable(newRole) {
		return Member{}, apperr.Validation(map[string]string{"role": "must be one of: admin, member"})
	}

	var changed Member
	err = s.tx.WithTx(ctx, func(tx pgx.Tx) error {
		target, err := s.memberOf(ctx, tx, orgID, targetID)
		if err != nil {
			return err
		}
		if !CanManageMember(actor, target.Role) {
			return apperr.Forbidden()
		}
		if target.Role == newRole {
			changed = target
			return nil
		}
		// An admin may only hand out what they could take back: promoting a
		// member to admin is the one change they are allowed.
		if actor == RoleAdmin && newRole != RoleAdmin {
			return apperr.Forbidden()
		}
		if _, err := orgdb.New(tx).UpdateMembershipRole(ctx, orgdb.UpdateMembershipRoleParams{
			OrganizationID: orgID, UserID: targetID, Role: string(newRole),
		}); err != nil {
			return fmt.Errorf("org: change role: %w", err)
		}
		target.Role = newRole
		changed = target
		return nil
	})
	return changed, err
}

// RemoveMember takes targetID out of the organization (spec §7.2). Members use
// Leave for themselves; the owner cannot be removed.
func (s *Service) RemoveMember(ctx context.Context, targetID uuid.UUID) error {
	if err := Authorize(ctx, ActionRemoveMember); err != nil {
		return err
	}
	orgID, err := tenant(ctx)
	if err != nil {
		return err
	}
	actor, _ := RoleFrom(ctx)
	return s.tx.WithTx(ctx, func(tx pgx.Tx) error {
		target, err := s.memberOf(ctx, tx, orgID, targetID)
		if err != nil {
			return err
		}
		if !CanManageMember(actor, target.Role) {
			return apperr.Forbidden()
		}
		return s.deleteMembership(ctx, tx, orgID, targetID)
	})
}

// Leave removes the caller from the current organization. Owners cannot leave:
// they must transfer ownership first (spec §7.2).
func (s *Service) Leave(ctx context.Context) error {
	role, _ := RoleFrom(ctx)
	if role == RoleOwner {
		return apperr.Forbidden().WithDetails(map[string]any{"reason": "owner_must_transfer_ownership"})
	}
	if err := Authorize(ctx, ActionLeave); err != nil {
		return err
	}
	orgID, err := tenant(ctx)
	if err != nil {
		return err
	}
	userID, ok := reqctx.UserID(ctx)
	if !ok {
		return apperr.Unauthenticated()
	}
	return s.tx.WithTx(ctx, func(tx pgx.Tx) error {
		return s.deleteMembership(ctx, tx, orgID, userID)
	})
}

func (s *Service) deleteMembership(ctx context.Context, tx pgx.Tx, orgID, userID uuid.UUID) error {
	n, err := orgdb.New(tx).DeleteMembership(ctx, orgdb.DeleteMembershipParams{OrganizationID: orgID, UserID: userID})
	if err != nil {
		return fmt.Errorf("org: delete membership: %w", err)
	}
	if n == 0 {
		return apperr.NotFound()
	}
	return nil
}
