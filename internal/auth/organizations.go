package auth

import (
	"context"

	"github.com/google/uuid"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/user"
)

// OrganizationGuard is what account deletion needs from the organization
// module (spec §6). The organization module implements it in Phase 4; auth
// declares it because auth orchestrates the deletion.
type OrganizationGuard interface {
	// BeforeUserDeleted runs inside the deletion transaction. It must return
	// ErrOwnsSharedOrganization when the user still owns an organization that
	// has other members; otherwise it removes the user from their organizations
	// and deletes (soft) the ones where they were the only member, cancelling
	// their subscriptions. Doing the check and the cleanup in one transaction
	// means nobody can join in between.
	BeforeUserDeleted(ctx context.Context, tx db.DBTX, userID uuid.UUID) error
}

// OrganizationProvisioner is what registration needs from the organization
// module (spec §5.2): every new user ends up in an organization.
type OrganizationProvisioner interface {
	// ProvisionForNewUser runs inside the registration transaction, after the
	// user row exists. It creates the user's own organization (they become its
	// owner), or joins the organizations that invited this email instead.
	ProvisionForNewUser(ctx context.Context, tx db.DBTX, u user.User, organizationName string) error
}

// ErrOwnsSharedOrganization is the CONFLICT an OrganizationGuard returns when
// deleting the account would orphan an organization that still has members.
func ErrOwnsSharedOrganization() error {
	return apperr.Conflict("Anda masih menjadi owner organisasi yang memiliki anggota lain. Transfer kepemilikan terlebih dahulu.").
		WithDetails(map[string]any{"reason": "owns_shared_organization"})
}

// NoOrganizations is the OrganizationGuard to use while organizations do not
// exist yet (before Phase 4): nobody owns anything, so nothing blocks deletion.
type NoOrganizations struct{}

// BeforeUserDeleted implements OrganizationGuard.
func (NoOrganizations) BeforeUserDeleted(context.Context, db.DBTX, uuid.UUID) error { return nil }

var _ OrganizationGuard = NoOrganizations{}
