// Package org owns organizations, memberships and invitations (spec §7). The
// organization is the tenant: other modules reach it only through Service and
// the tenant context the middleware puts on the request context.
package org

import (
	"time"

	"github.com/google/uuid"

	"github.com/kangsolder001/saka/internal/org/orgdb"
	"github.com/kangsolder001/saka/internal/platform/clock"
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

// Service is the public API of the organization module.
type Service struct {
	clk clock.Clock
}

// NewService creates a Service.
func NewService(clk clock.Clock) *Service {
	return &Service{clk: clk}
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
