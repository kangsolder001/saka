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

// maxSlugAttempts bounds the search for a free slug when none was requested.
const maxSlugAttempts = 8

// CreateWithOwner inserts an organization and the owner membership of ownerID
// using the caller's transaction, so it commits or rolls back together with
// whatever else the caller does (registration, POST /orgs). q must be a
// transaction: the organization's owner foreign key is deferred to commit, so
// outside one the first insert would fail. When slug is empty
// one is derived from the name, with a random suffix if that is taken; an
// explicit slug that is taken returns CONFLICT.
func (s *Service) CreateWithOwner(ctx context.Context, q db.DBTX, ownerID uuid.UUID, name, slug string) (Organization, error) {
	name = strings.TrimSpace(name)
	fields := map[string]string{}
	switch {
	case name == "":
		fields["name"] = "is required"
	case utf8.RuneCountInString(name) > MaxNameLength:
		fields["name"] = fmt.Sprintf("must be at most %d characters", MaxNameLength)
	}
	explicit := strings.ToLower(strings.TrimSpace(slug))
	if explicit != "" && !ValidSlug(explicit) {
		fields["slug"] = fmt.Sprintf("must be %d-%d lowercase letters, digits or hyphens", MinSlugLength, MaxSlugLength)
	}
	if len(fields) > 0 {
		return Organization{}, apperr.Validation(fields)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return Organization{}, fmt.Errorf("org: new id: %w", err)
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
// own. It runs inside the registration transaction (auth declares the
// interface it satisfies).
func (s *Service) ProvisionForNewUser(ctx context.Context, q db.DBTX, u user.User, organizationName string) error {
	_, err := s.CreateWithOwner(ctx, q, u.ID, organizationName, "")
	return err
}
