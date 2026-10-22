package org

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/org/orgdb"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/token"
	"github.com/kangsolder001/saka/internal/user"
)

// maxInvitationTokenLength bounds the token accepted from a URL; real tokens
// are 43 characters, so anything longer is rejected before hashing.
const maxInvitationTokenLength = 256

// InvitationInfo is what the person holding an invitation link may see before
// deciding (spec §7.1 GET /invitations/{token}).
type InvitationInfo struct {
	Email            string
	Role             Role
	OrganizationName string
	OrganizationLogo *string
	InviterName      string
	ExpiresAt        time.Time
	// AccountExists tells the frontend whether to send the visitor to log in or
	// to register (spec §7.4). The token holder already owns this email address,
	// so this reveals nothing to them they do not know.
	AccountExists bool
}

// invitationState is where an invitation stands.
type invitationState string

const (
	stateOpen     invitationState = "open"
	stateExpired  invitationState = "expired"
	stateAccepted invitationState = "accepted"
	stateDeclined invitationState = "declined"
	stateRevoked  invitationState = "revoked"
	stateGone     invitationState = "organization_deleted"
)

// stateOf classifies an invitation row at time now.
func stateOf(accepted, declined, revoked, orgDeleted *time.Time, expires, now time.Time) invitationState {
	switch {
	case accepted != nil:
		return stateAccepted
	case declined != nil:
		return stateDeclined
	case revoked != nil:
		return stateRevoked
	case orgDeleted != nil:
		return stateGone
	case !expires.After(now):
		return stateExpired
	default:
		return stateOpen
	}
}

// errInvitationUnusable is the NOT_FOUND for a link that no longer works. The
// reason helps the person holding the link (they got it by email, so they are
// entitled to know it expired) but is absent for tokens that do not exist.
func errInvitationUnusable(state invitationState) error {
	return apperr.NotFound().WithDetails(map[string]any{"reason": string(state)})
}

// lookupInvitation reads (and optionally row-locks) the invitation behind a raw
// token, inside a system transaction: the token is the credential and the
// organization is not known yet.
func (s *Service) lookupInvitation(ctx context.Context, tx pgx.Tx, raw string, lock bool) (orgdb.GetInvitationByTokenHashRow, invitationState, error) {
	if raw == "" || len(raw) > maxInvitationTokenLength {
		return orgdb.GetInvitationByTokenHashRow{}, "", apperr.NotFound()
	}
	q := orgdb.New(tx)
	hash := token.Hash(raw)
	var (
		row orgdb.GetInvitationByTokenHashRow
		err error
	)
	if lock {
		var locked orgdb.GetInvitationByTokenHashForUpdateRow
		locked, err = q.GetInvitationByTokenHashForUpdate(ctx, hash)
		row = orgdb.GetInvitationByTokenHashRow(locked)
	} else {
		row, err = q.GetInvitationByTokenHash(ctx, hash)
	}
	if err != nil {
		if isNoRows(err) {
			return row, "", apperr.NotFound()
		}
		return row, "", fmt.Errorf("org: look up invitation: %w", err)
	}
	return row, stateOf(row.AcceptedAt, row.DeclinedAt, row.RevokedAt, row.OrganizationDeletedAt, row.ExpiresAt, s.clk.Now()), nil
}

// InvitationByToken returns what the link's holder may see (public: the token
// is the credential). Links that no longer work are NOT_FOUND, with the reason
// in details.reason when the token is known.
func (s *Service) InvitationByToken(ctx context.Context, raw string) (InvitationInfo, error) {
	var info InvitationInfo
	err := s.tx.WithSystemTx(ctx, func(tx pgx.Tx) error {
		row, state, err := s.lookupInvitation(ctx, tx, raw, false)
		if err != nil {
			return err
		}
		if state != stateOpen {
			return errInvitationUnusable(state)
		}
		people, err := s.Users.GetSummaries(ctx, tx, []uuid.UUID{row.InvitedBy})
		if err != nil {
			return err
		}
		_, lookup := s.Users.GetSummaryByEmail(ctx, tx, row.Email)
		if lookup != nil && !apperr.IsCode(lookup, apperr.CodeNotFound) {
			return lookup
		}
		info = InvitationInfo{
			Email: row.Email, Role: Role(row.Role), OrganizationName: row.OrganizationName,
			OrganizationLogo: row.OrganizationLogoUrl, InviterName: people[row.InvitedBy].Name,
			ExpiresAt: row.ExpiresAt, AccountExists: lookup == nil,
		}
		return nil
	})
	return info, err
}

// AcceptInvitation makes userID a member of the invitation's organization with
// the invited role (spec §7.4). The user's email must be the one invited. In one
// transaction the invitation is locked, checked, closed and the membership
// inserted, so a link works exactly once even when used concurrently. A user who
// is already a member simply closes the invitation and keeps their role.
func (s *Service) AcceptInvitation(ctx context.Context, userID uuid.UUID, raw string) (MyOrganization, error) {
	var joined MyOrganization
	err := s.tx.WithSystemTx(ctx, func(tx pgx.Tx) error {
		row, state, err := s.lookupInvitation(ctx, tx, raw, true)
		if err != nil {
			return err
		}
		if state != stateOpen {
			return errInvitationUnusable(state)
		}
		people, err := s.Users.GetSummaries(ctx, tx, []uuid.UUID{userID})
		if err != nil {
			return err
		}
		me, ok := people[userID]
		if !ok {
			return apperr.Unauthenticated()
		}
		if user.NormalizeEmail(me.Email) != user.NormalizeEmail(row.Email) {
			return apperr.Forbidden().WithDetails(map[string]any{"reason": "invitation_email_mismatch"})
		}

		q := orgdb.New(tx)
		if _, err := q.InsertMembershipIfAbsent(ctx, orgdb.InsertMembershipIfAbsentParams{
			OrganizationID: row.OrganizationID, UserID: userID, Role: row.Role, Now: s.clk.Now(),
		}); err != nil {
			return fmt.Errorf("org: insert membership: %w", err)
		}
		if n, err := q.MarkInvitationAccepted(ctx, orgdb.MarkInvitationAcceptedParams{ID: row.ID, Now: s.clk.Now()}); err != nil {
			return fmt.Errorf("org: mark accepted: %w", err)
		} else if n == 0 {
			return errInvitationUnusable(stateAccepted)
		}

		org, err := q.GetOrganization(ctx, row.OrganizationID)
		if err != nil {
			return fmt.Errorf("org: load organization: %w", err)
		}
		membership, err := q.GetMembership(ctx, orgdb.GetMembershipParams{OrganizationID: row.OrganizationID, UserID: userID})
		if err != nil {
			return fmt.Errorf("org: load membership: %w", err)
		}
		joined = MyOrganization{
			Organization: organizationFromRow(org), Role: Role(membership.Role),
			MembershipID: membership.ID, JoinedAt: membership.CreatedAt,
		}
		return nil
	})
	return joined, err
}

// DeclineInvitation closes the invitation without joining. Like looking it up,
// it needs no login: the link is the credential, and the person it was sent to
// may not have an account.
func (s *Service) DeclineInvitation(ctx context.Context, raw string) error {
	return s.tx.WithSystemTx(ctx, func(tx pgx.Tx) error {
		row, state, err := s.lookupInvitation(ctx, tx, raw, true)
		if err != nil {
			return err
		}
		if state != stateOpen {
			return errInvitationUnusable(state)
		}
		if n, err := orgdb.New(tx).MarkInvitationDeclined(ctx, orgdb.MarkInvitationDeclinedParams{ID: row.ID, Now: s.clk.Now()}); err != nil {
			return fmt.Errorf("org: mark declined: %w", err)
		} else if n == 0 {
			return errInvitationUnusable(stateDeclined)
		}
		return nil
	})
}
