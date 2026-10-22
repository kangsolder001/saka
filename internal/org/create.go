package org

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/kangsolder001/saka/internal/org/orgdb"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/user"
)

// MaxNameLength is the longest organization name accepted.
const MaxNameLength = 100

// nameProblem returns why a trimmed organization name is unacceptable, or "".
func nameProblem(name string) string {
	switch {
	case name == "":
		return "is required"
	case utf8.RuneCountInString(name) > MaxNameLength:
		return fmt.Sprintf("must be at most %d characters", MaxNameLength)
	}
	return ""
}

var slugProblem = fmt.Sprintf("must be %d-%d lowercase letters, digits or hyphens", MinSlugLength, MaxSlugLength)

// maxSlugAttempts bounds the search for a free slug when none was requested.
const maxSlugAttempts = 8

// CreateWithOwner inserts an organization, the owner membership of ownerID and
// the organization's Free subscription using the caller's transaction, so it commits or rolls back together with
// whatever else the caller does (registration, POST /orgs). q must be a
// transaction: the organization's owner foreign key is deferred to commit, so
// outside one the first insert would fail. When slug is empty
// one is derived from the name, with a random suffix if that is taken; an
// explicit slug that is taken returns CONFLICT.
func (s *Service) CreateWithOwner(ctx context.Context, q db.DBTX, ownerID uuid.UUID, name, slug string) (Organization, error) {
	name = strings.TrimSpace(name)
	fields := map[string]string{}
	if reason := nameProblem(name); reason != "" {
		fields["name"] = reason
	}
	explicit := strings.ToLower(strings.TrimSpace(slug))
	if explicit != "" && !ValidSlug(explicit) {
		fields["slug"] = slugProblem
	}
	if len(fields) > 0 {
		return Organization{}, apperr.Validation(fields)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return Organization{}, fmt.Errorf("org: new id: %w", err)
	}
	// The organization does not exist yet, so no tenant is on the context; pin
	// the new one so its own rows pass the row-level security policy.
	if err := db.PinTenant(ctx, q, id); err != nil {
		return Organization{}, fmt.Errorf("org: %w", err)
	}
	row, err := s.insertOrganization(ctx, q, id, ownerID, name, explicit)
	if err != nil {
		return Organization{}, err
	}
	if _, err := orgdbQueries(q).InsertMembership(ctx, orgdb.InsertMembershipParams{
		OrganizationID: row.ID,
		UserID:         ownerID,
		Role:           string(RoleOwner),
		Now:            s.clk.Now(),
	}); err != nil {
		return Organization{}, fmt.Errorf("org: insert owner membership: %w", err)
	}
	if err := s.Subscriptions.StartFree(ctx, q, row.ID); err != nil {
		return Organization{}, fmt.Errorf("org: %w", err)
	}
	return organizationFromRow(row), nil
}

// insertOrganization finds a free slug. ON CONFLICT DO NOTHING means a taken
// slug returns no row rather than an error, so the transaction stays usable.
func (s *Service) insertOrganization(ctx context.Context, q db.DBTX, id, ownerID uuid.UUID, name, explicit string) (orgdb.Organization, error) {
	candidate := explicit
	if candidate == "" {
		candidate = Slugify(name)
	}
	for attempt := 0; attempt < maxSlugAttempts; attempt++ {
		row, err := orgdbQueries(q).InsertOrganization(ctx, orgdb.InsertOrganizationParams{
			ID: id, Name: name, Slug: candidate, OwnerID: ownerID, Now: s.clk.Now(),
		})
		if err == nil {
			return row, nil
		}
		if !isNoRows(err) {
			return orgdb.Organization{}, fmt.Errorf("org: insert organization: %w", err)
		}
		if explicit != "" {
			return orgdb.Organization{}, apperr.Conflict("Slug sudah dipakai.")
		}
		candidate = withSuffix(Slugify(name))
	}
	return orgdb.Organization{}, fmt.Errorf("org: no free slug for %q after %d attempts", name, maxSlugAttempts)
}

// ProvisionForNewUser gives a freshly registered user an organization they
// own, inside the registration transaction (auth declares the interface it
// satisfies). When the address has been invited somewhere (spec §7.4) no
// organization is created: the user is going to join the inviting one(s). The
// join itself waits for email verification (AcceptPendingInvitations).
func (s *Service) ProvisionForNewUser(ctx context.Context, q db.DBTX, u user.User, organizationName string) error {
	var invited bool
	err := db.WithBypass(ctx, q, func() error {
		var err error
		invited, err = orgdbQueries(q).HasLiveInvitationForEmail(ctx, orgdb.HasLiveInvitationForEmailParams{
			Email: user.NormalizeEmail(u.Email), Now: s.clk.Now(),
		})
		return err
	})
	if err != nil {
		return fmt.Errorf("org: check invitations: %w", err)
	}
	if invited {
		return nil
	}
	_, err = s.CreateWithOwner(ctx, q, u.ID, organizationName, "")
	return err
}

// AcceptPendingInvitations joins userID to every organization that invited
// email and whose invitation is still live, and closes those invitations. Auth
// calls it, in the same transaction, when the address has just been proven
// (email verification, confirmed email change): until then anyone could sign up
// with an invited address and walk into its organization, so registration alone
// does not join anything. Joining is idempotent: someone already a member keeps
// their role.
func (s *Service) AcceptPendingInvitations(ctx context.Context, q db.DBTX, userID uuid.UUID, email string) error {
	return db.WithBypass(ctx, q, func() error {
		queries := orgdbQueries(q)
		live, err := queries.ListLiveInvitationsForEmailForUpdate(ctx, orgdb.ListLiveInvitationsForEmailForUpdateParams{
			Email: user.NormalizeEmail(email), Now: s.clk.Now(),
		})
		if err != nil {
			return fmt.Errorf("org: list live invitations: %w", err)
		}
		for _, inv := range live {
			if _, err := queries.InsertMembershipIfAbsent(ctx, orgdb.InsertMembershipIfAbsentParams{
				OrganizationID: inv.OrganizationID, UserID: userID, Role: inv.Role, Now: s.clk.Now(),
			}); err != nil {
				return fmt.Errorf("org: join invited organization: %w", err)
			}
			if _, err := queries.MarkInvitationAccepted(ctx, orgdb.MarkInvitationAcceptedParams{ID: inv.ID, Now: s.clk.Now()}); err != nil {
				return fmt.Errorf("org: close invitation: %w", err)
			}
		}
		return nil
	})
}
