package org

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kangsolder001/saka/internal/org/orgdb"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/pagination"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
	"github.com/kangsolder001/saka/internal/platform/validate"
	"github.com/kangsolder001/saka/internal/user"
)

// MaxLogoURLLength bounds the logo URL, like avatars.
const MaxLogoURLLength = 2048

// MyOrganization is an organization as seen by one of its members.
type MyOrganization struct {
	Organization
	Role         Role
	MembershipID uuid.UUID
	JoinedAt     time.Time
}

// CursorOf is the pagination cursor position of an item in ListForUser.
func (m MyOrganization) CursorOf() pagination.Cursor {
	return pagination.Cursor{CreatedAt: m.JoinedAt, ID: m.MembershipID}
}

// ListForUser returns the organizations userID belongs to, oldest membership
// first. It returns up to p.FetchLimit() rows, one more than the page size, so
// the caller can build the next cursor with pagination.Paginate. It is
// deliberately not tenant-scoped: it answers "which tenants do I have".
func (s *Service) ListForUser(ctx context.Context, userID uuid.UUID, p pagination.Params) ([]MyOrganization, error) {
	params := orgdb.ListOrganizationsForUserParams{UserID: userID, RowLimit: int32(p.FetchLimit())} //nolint:gosec // bounded by pagination.MaxLimit
	if p.Cursor != nil {
		params.AfterCreatedAt, params.AfterID = &p.Cursor.CreatedAt, &p.Cursor.ID
	}
	var out []MyOrganization
	err := s.tx.WithTx(ctx, func(tx pgx.Tx) error {
		rows, err := orgdb.New(tx).ListOrganizationsForUser(ctx, params)
		if err != nil {
			return fmt.Errorf("org: list for user: %w", err)
		}
		for _, r := range rows {
			out = append(out, MyOrganization{
				Organization: Organization{
					ID: r.ID, Name: r.Name, Slug: r.Slug, LogoURL: r.LogoUrl, OwnerID: r.OwnerID,
					CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
				},
				Role: Role(r.Role), MembershipID: r.MembershipID, JoinedAt: r.JoinedAt,
			})
		}
		return nil
	})
	return out, err
}

// Create makes a new organization owned by userID (POST /orgs). A blank slug
// is derived from the name.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, name, slug string) (Organization, error) {
	var created Organization
	err := s.tx.WithTx(ctx, func(tx pgx.Tx) error {
		var err error
		created, err = s.CreateWithOwner(ctx, tx, userID, name, slug)
		return err
	})
	return created, err
}

// tenant returns the organization ID of the current request, which only the
// tenant middleware can have put on the context.
func tenant(ctx context.Context) (uuid.UUID, error) {
	id, ok := reqctx.OrgID(ctx)
	if !ok {
		return uuid.Nil, apperr.NotFound()
	}
	return id, nil
}

// Get returns the current organization (the tenant on ctx).
func (s *Service) Get(ctx context.Context) (Organization, error) {
	if err := Authorize(ctx, ActionView); err != nil {
		return Organization{}, err
	}
	id, err := tenant(ctx)
	if err != nil {
		return Organization{}, err
	}
	var found Organization
	err = s.tx.WithTx(ctx, func(tx pgx.Tx) error {
		row, err := orgdb.New(tx).GetOrganization(ctx, id)
		if err != nil {
			if isNoRows(err) {
				return apperr.NotFound()
			}
			return fmt.Errorf("org: get: %w", err)
		}
		found = organizationFromRow(row)
		return nil
	})
	return found, err
}

// Update is a partial change to the organization's settings (PATCH).
type Update struct {
	Name *string
	Slug *string
	// SetLogo says LogoURL is part of the update; LogoURL nil clears the logo.
	SetLogo bool
	LogoURL *string
}

// Update changes the current organization's name, slug and/or logo. Owners and
// admins only. A slug that is taken returns CONFLICT.
func (s *Service) Update(ctx context.Context, in Update) (Organization, error) {
	if err := Authorize(ctx, ActionUpdateSettings); err != nil {
		return Organization{}, err
	}
	id, err := tenant(ctx)
	if err != nil {
		return Organization{}, err
	}
	if in.Name == nil && in.Slug == nil && !in.SetLogo {
		return Organization{}, apperr.Validation(map[string]string{"body": "must contain at least one field to change"})
	}

	params := orgdb.UpdateOrganizationParams{ID: id, Now: s.clk.Now(), SetLogo: in.SetLogo}
	fields := map[string]string{}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if reason := nameProblem(name); reason != "" {
			fields["name"] = reason
		} else {
			params.Name = &name
		}
	}
	if in.Slug != nil {
		slug := strings.ToLower(strings.TrimSpace(*in.Slug))
		if !ValidSlug(slug) {
			fields["slug"] = slugProblem
		} else {
			params.Slug = &slug
		}
	}
	if in.SetLogo && in.LogoURL != nil {
		logo := strings.TrimSpace(*in.LogoURL)
		if reason := validate.HTTPURLProblem(logo, MaxLogoURLLength); reason != "" {
			fields["logo_url"] = reason
		} else {
			params.LogoUrl = &logo
		}
	}
	if len(fields) > 0 {
		return Organization{}, apperr.Validation(fields)
	}

	var updated Organization
	err = s.tx.WithTx(ctx, func(tx pgx.Tx) error {
		row, err := orgdb.New(tx).UpdateOrganization(ctx, params)
		if err != nil {
			var pgErr *pgconn.PgError
			switch {
			case isNoRows(err):
				return apperr.NotFound()
			case errors.As(err, &pgErr) && pgErr.Code == "23505":
				return apperr.Conflict("Slug sudah dipakai.")
			}
			return fmt.Errorf("org: update: %w", err)
		}
		updated = organizationFromRow(row)
		return nil
	})
	return updated, err
}

// Delete soft-deletes the current organization and revokes its open
// invitations, in one transaction. Owners only. Memberships stay, so history is
// intact; the tenant lookup treats a deleted organization as unknown, so
// every member loses access at once.
func (s *Service) Delete(ctx context.Context) error {
	if err := Authorize(ctx, ActionDeleteOrg); err != nil {
		return err
	}
	id, err := tenant(ctx)
	if err != nil {
		return err
	}
	return s.tx.WithTx(ctx, func(tx pgx.Tx) error {
		q := orgdb.New(tx)
		n, err := q.SoftDeleteOrganization(ctx, orgdb.SoftDeleteOrganizationParams{ID: id, Now: s.clk.Now()})
		if err != nil {
			return fmt.Errorf("org: delete: %w", err)
		}
		if n == 0 {
			return apperr.NotFound()
		}
		if _, err := q.RevokeOpenInvitationsForOrganization(ctx, orgdb.RevokeOpenInvitationsForOrganizationParams{
			OrganizationID: id, Now: s.clk.Now(),
		}); err != nil {
			return fmt.Errorf("org: revoke invitations: %w", err)
		}
		return nil
	})
}

// maxOrganizationsInProfile caps the list GET /me returns. A user belongs to a
// handful of organizations; the cap only guards against pathological data.
const maxOrganizationsInProfile = 100

// OrganizationsOf lists the organizations userID belongs to for GET /me; it
// implements user.OrganizationLister.
func (s *Service) OrganizationsOf(ctx context.Context, userID uuid.UUID) ([]user.OrganizationRef, error) {
	rows, err := s.ListForUser(ctx, userID, pagination.Params{Limit: maxOrganizationsInProfile - 1})
	if err != nil {
		return nil, err
	}
	out := make([]user.OrganizationRef, len(rows))
	for i, r := range rows {
		out[i] = user.OrganizationRef{ID: r.ID, Name: r.Name, Slug: r.Slug, Role: string(r.Role)}
	}
	return out, nil
}

var _ user.OrganizationLister = (*Service)(nil)
