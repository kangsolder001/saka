package org

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/mail"
	"github.com/kangsolder001/saka/internal/org/orgdb"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/pagination"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
	"github.com/kangsolder001/saka/internal/platform/token"
	"github.com/kangsolder001/saka/internal/user"
)

// InvitationTTL is how long an invitation stays valid (spec §4.6).
const InvitationTTL = 7 * 24 * time.Hour

// Invitation is an offer to join an organization. The token that proves
// possession of the email is never part of it: only its hash is stored.
type Invitation struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	Email          string
	Role           Role
	InvitedBy      uuid.UUID
	ExpiresAt      time.Time
	CreatedAt      time.Time
}

// CursorOf is the pagination cursor position of an item in ListInvitations.
func (i Invitation) CursorOf() pagination.Cursor {
	return pagination.Cursor{CreatedAt: i.CreatedAt, ID: i.ID}
}

func invitationFromRow(r orgdb.Invitation) Invitation {
	return Invitation{
		ID: r.ID, OrganizationID: r.OrganizationID, Email: r.Email, Role: Role(r.Role),
		InvitedBy: r.InvitedBy, ExpiresAt: r.ExpiresAt, CreatedAt: r.CreatedAt,
	}
}

// roleLabel is how a role reads in the invitation email.
func roleLabel(r Role) string {
	if r == RoleAdmin {
		return "Admin"
	}
	return "Anggota"
}

// Invite offers email a place in the current organization with the given role
// and queues the invitation email, in one transaction (spec §7.4). Owners and
// admins only. Inviting an address that already has an open invitation
// refreshes that invitation (new token, new expiry) rather than adding a
// second; refreshed reports that case. Someone who is already a member is a
// CONFLICT.
func (s *Service) Invite(ctx context.Context, email string, role Role) (inv Invitation, refreshed bool, err error) {
	if err := Authorize(ctx, ActionInvite); err != nil {
		return Invitation{}, false, err
	}
	orgID, err := tenant(ctx)
	if err != nil {
		return Invitation{}, false, err
	}
	actorID, ok := reqctx.UserID(ctx)
	if !ok {
		return Invitation{}, false, apperr.Unauthenticated()
	}
	email = user.NormalizeEmail(email)
	fields := map[string]string{}
	if email == "" {
		fields["email"] = "is required"
	}
	if !Assignable(role) {
		fields["role"] = "must be one of: admin, member"
	}
	if len(fields) > 0 {
		return Invitation{}, false, apperr.Validation(fields)
	}
	raw, hash, err := token.Generate()
	if err != nil {
		return Invitation{}, false, err
	}

	err = s.tx.WithTx(ctx, func(tx pgx.Tx) error {
		q := orgdb.New(tx)
		if err := s.refuseExistingMember(ctx, tx, orgID, email); err != nil {
			return err
		}
		_, lookup := q.GetOpenInvitationByEmailForUpdate(ctx, orgdb.GetOpenInvitationByEmailForUpdateParams{OrganizationID: orgID, Email: email})
		switch {
		case lookup == nil:
			refreshed = true
		case !isNoRows(lookup):
			return fmt.Errorf("org: look up open invitation: %w", lookup)
		}
		row, err := q.UpsertInvitation(ctx, orgdb.UpsertInvitationParams{
			OrganizationID: orgID, Email: email, Role: string(role), TokenHash: hash, InvitedBy: actorID,
			ExpiresAt: s.clk.Now().Add(InvitationTTL), Now: s.clk.Now(),
		})
		if err != nil {
			return fmt.Errorf("org: upsert invitation: %w", err)
		}
		inv = invitationFromRow(row)
		return s.sendInvitationEmail(ctx, tx, inv, actorID, raw)
	})
	if err != nil {
		return Invitation{}, false, err
	}
	return inv, refreshed, nil
}

// refuseExistingMember returns CONFLICT when email already belongs to a member
// of the organization (spec §7.4: only non-members can be invited).
func (s *Service) refuseExistingMember(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, email string) error {
	existing, err := s.Users.GetSummaryByEmail(ctx, tx, email)
	if err != nil {
		if apperr.IsCode(err, apperr.CodeNotFound) {
			return nil // no account yet: certainly not a member
		}
		return err
	}
	_, err = orgdb.New(tx).GetMembership(ctx, orgdb.GetMembershipParams{OrganizationID: orgID, UserID: existing.ID})
	switch {
	case err == nil:
		return apperr.Conflict("Pengguna ini sudah menjadi anggota organisasi.")
	case isNoRows(err):
		return nil
	default:
		return fmt.Errorf("org: check membership: %w", err)
	}
}

// sendInvitationEmail queues the email for inv carrying the raw token, using
// the caller's transaction.
func (s *Service) sendInvitationEmail(ctx context.Context, tx pgx.Tx, inv Invitation, inviterID uuid.UUID, rawToken string) error {
	orgRow, err := orgdb.New(tx).GetOrganization(ctx, inv.OrganizationID)
	if err != nil {
		return fmt.Errorf("org: load organization for invitation: %w", err)
	}
	people, err := s.Users.GetSummaries(ctx, tx, []uuid.UUID{inviterID})
	if err != nil {
		return err
	}
	inviter := people[inviterID]
	inviterName := inviter.Name
	if inviterName == "" {
		inviterName = "Seorang anggota tim"
	}
	return s.Mail.Send(ctx, tx, mail.Email{
		Template: "invitation",
		To:       inv.Email,
		Data: map[string]any{
			"inviter_name":      inviterName,
			"organization_name": orgRow.Name,
			"role_label":        roleLabel(inv.Role),
			"expires_in_days":   int(InvitationTTL.Hours() / 24),
			"url":               s.FrontendURL + "/accept-invitation?token=" + url.QueryEscape(rawToken),
		},
	})
}

// ListInvitations returns the current organization's active invitations (open
// and not expired), oldest first. It returns up to p.FetchLimit() rows so the
// caller can paginate. Owners and admins only.
func (s *Service) ListInvitations(ctx context.Context, p pagination.Params) ([]Invitation, error) {
	if err := Authorize(ctx, ActionInvite); err != nil {
		return nil, err
	}
	orgID, err := tenant(ctx)
	if err != nil {
		return nil, err
	}
	params := orgdb.ListActiveInvitationsParams{OrganizationID: orgID, Now: s.clk.Now(), RowLimit: int32(p.FetchLimit())} //nolint:gosec // bounded by pagination.MaxLimit
	if p.Cursor != nil {
		params.AfterCreatedAt, params.AfterID = &p.Cursor.CreatedAt, &p.Cursor.ID
	}
	var out []Invitation
	err = s.tx.WithTx(ctx, func(tx pgx.Tx) error {
		rows, err := orgdb.New(tx).ListActiveInvitations(ctx, params)
		if err != nil {
			return fmt.Errorf("org: list invitations: %w", err)
		}
		for _, r := range rows {
			out = append(out, invitationFromRow(r))
		}
		return nil
	})
	return out, err
}

// RevokeInvitation cancels an open invitation of the current organization; its
// link stops working. An unknown, already used or already cancelled invitation
// is NOT_FOUND.
func (s *Service) RevokeInvitation(ctx context.Context, id uuid.UUID) error {
	if err := Authorize(ctx, ActionInvite); err != nil {
		return err
	}
	orgID, err := tenant(ctx)
	if err != nil {
		return err
	}
	return s.tx.WithTx(ctx, func(tx pgx.Tx) error {
		n, err := orgdb.New(tx).RevokeInvitation(ctx, orgdb.RevokeInvitationParams{ID: id, OrganizationID: orgID, Now: s.clk.Now()})
		if err != nil {
			return fmt.Errorf("org: revoke invitation: %w", err)
		}
		if n == 0 {
			return apperr.NotFound()
		}
		return nil
	})
}

// ResendInvitation gives an open invitation a fresh token and expiry and
// emails it again; the previous link stops working. Expired invitations can be
// resent, accepted, declined or cancelled ones cannot (NOT_FOUND).
func (s *Service) ResendInvitation(ctx context.Context, id uuid.UUID) (Invitation, error) {
	if err := Authorize(ctx, ActionInvite); err != nil {
		return Invitation{}, err
	}
	orgID, err := tenant(ctx)
	if err != nil {
		return Invitation{}, err
	}
	actorID, ok := reqctx.UserID(ctx)
	if !ok {
		return Invitation{}, apperr.Unauthenticated()
	}
	raw, hash, err := token.Generate()
	if err != nil {
		return Invitation{}, err
	}

	var inv Invitation
	err = s.tx.WithTx(ctx, func(tx pgx.Tx) error {
		row, err := orgdb.New(tx).RenewInvitation(ctx, orgdb.RenewInvitationParams{
			ID: id, OrganizationID: orgID, TokenHash: hash, ExpiresAt: s.clk.Now().Add(InvitationTTL),
		})
		if err != nil {
			if isNoRows(err) {
				return apperr.NotFound()
			}
			return fmt.Errorf("org: renew invitation: %w", err)
		}
		inv = invitationFromRow(row)
		return s.sendInvitationEmail(ctx, tx, inv, actorID, raw)
	})
	return inv, err
}
