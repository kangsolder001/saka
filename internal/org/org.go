// Package org owns organizations, memberships and invitations (spec §7). The
// organization is the tenant: other modules reach it only through Service and
// the tenant context the middleware puts on the request context.
package org

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/org/orgdb"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/user"
)

// Role is a member's role inside one organization (memberships.role).
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

// Organization is a tenant.
type Organization struct {
	ID        uuid.UUID
	Name      string
	Slug      string
	LogoURL   *string
	OwnerID   uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Membership is a user's place in one organization.
type Membership struct {
	OrganizationID uuid.UUID
	UserID         uuid.UUID
	Role           Role
	CreatedAt      time.Time
}

// Transactor runs a function in a database transaction; *db.DB implements it.
type Transactor interface {
	// WithTx runs fn in a transaction pinned to the tenant on ctx, if any.
	WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error
	// WithSystemTx runs fn in a transaction that sees every tenant. Only the
	// few flows that are cross-tenant by nature use it.
	WithSystemTx(ctx context.Context, fn func(tx pgx.Tx) error) error
}

var _ Transactor = (*db.DB)(nil)

// UserDirectory is what the organization module needs from the user module
// (*user.Service implements it); org never queries the users table itself.
type UserDirectory interface {
	GetSummaries(ctx context.Context, q db.DBTX, ids []uuid.UUID) (map[uuid.UUID]user.Summary, error)
}

var _ UserDirectory = (*user.Service)(nil)

// PasswordConfirmer checks a user's current password before a sensitive
// action (transferring ownership). auth.Service implements it, with its
// failed-guess rate limit; org declares it so the modules stay decoupled.
type PasswordConfirmer interface {
	// ConfirmPassword returns a VALIDATION_ERROR on "current_password" when
	// the password is wrong, and RATE_LIMITED after too many wrong guesses.
	ConfirmPassword(ctx context.Context, userID uuid.UUID, password string) error
}

// Dependencies are the collaborators of Service, injected by the composition root.
type Dependencies struct {
	Clock clock.Clock
	// Tx runs the transactions of methods that open their own (those that do
	// not take a db.DBTX).
	Tx        Transactor
	Users     UserDirectory
	Passwords PasswordConfirmer
}

// Service is the public API of the organization module.
type Service struct {
	Dependencies
	clk clock.Clock
	tx  Transactor
}

// NewService creates a Service.
func NewService(deps Dependencies) *Service {
	return &Service{Dependencies: deps, clk: deps.Clock, tx: deps.Tx}
}

func organizationFromRow(r orgdb.Organization) Organization {
	return Organization{
		ID:        r.ID,
		Name:      r.Name,
		Slug:      r.Slug,
		LogoURL:   r.LogoUrl,
		OwnerID:   r.OwnerID,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}

func membershipFromRow(r orgdb.Membership) Membership {
	return Membership{OrganizationID: r.OrganizationID, UserID: r.UserID, Role: Role(r.Role), CreatedAt: r.CreatedAt}
}
