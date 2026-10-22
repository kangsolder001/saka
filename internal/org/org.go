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
	WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error
}

var _ Transactor = (*db.DB)(nil)

// Service is the public API of the organization module.
type Service struct {
	clk clock.Clock
	tx  Transactor
}

// NewService creates a Service. tx runs the transactions of methods that open
// their own (those that do not take a db.DBTX).
func NewService(clk clock.Clock, tx Transactor) *Service {
	return &Service{clk: clk, tx: tx}
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
