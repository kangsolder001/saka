package org

import (
	"context"

	"github.com/gin-gonic/gin"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/httpx"
)

// Action is something a member may or may not do inside an organization.
// The set mirrors the permission matrix of spec §7.2.
type Action string

const (
	ActionView              Action = "view"               // organization and its members
	ActionUpdateSettings    Action = "update_settings"    // name, slug, logo
	ActionInvite            Action = "invite"             // invite, revoke and resend invitations
	ActionChangeRole        Action = "change_role"        // member <-> admin (see CanManageMember)
	ActionRemoveMember      Action = "remove_member"      // see CanManageMember
	ActionManageBilling     Action = "manage_billing"     // checkout, change plan, cancel, resume
	ActionTransferOwnership Action = "transfer_ownership" // hand the organization to another member
	ActionDeleteOrg         Action = "delete_org"
	ActionLeave             Action = "leave" // owners must transfer ownership first
)

// permissions is spec §7.2 as data. Actions that depend on the target member
// (change role, remove) are granted here at the level of "may ever do this";
// CanManageMember narrows them down to the allowed targets.
var permissions = map[Action]map[Role]bool{
	ActionView:              {RoleOwner: true, RoleAdmin: true, RoleMember: true},
	ActionUpdateSettings:    {RoleOwner: true, RoleAdmin: true},
	ActionInvite:            {RoleOwner: true, RoleAdmin: true},
	ActionChangeRole:        {RoleOwner: true, RoleAdmin: true},
	ActionRemoveMember:      {RoleOwner: true, RoleAdmin: true},
	ActionManageBilling:     {RoleOwner: true},
	ActionTransferOwnership: {RoleOwner: true},
	ActionDeleteOrg:         {RoleOwner: true},
	ActionLeave:             {RoleAdmin: true, RoleMember: true},
}

// Can reports whether role may perform action. Unknown roles and unknown
// actions are denied.
func Can(role Role, action Action) bool {
	return permissions[action][role]
}

// CanManageMember reports whether actor may change the role of, or remove, a
// member who currently has the target role. An owner can manage admins and
// members; an admin can manage plain members only (never other admins or the
// owner); a member manages nobody; nobody manages the owner (ownership moves
// only through a transfer).
func CanManageMember(actor, target Role) bool {
	switch actor {
	case RoleOwner:
		return target == RoleAdmin || target == RoleMember
	case RoleAdmin:
		return target == RoleMember
	default:
		return false
	}
}

// Assignable reports whether role can be given to a member by a role change
// or an invitation: owner is never assignable, it only moves by transfer.
func Assignable(role Role) bool {
	return role == RoleAdmin || role == RoleMember
}

// Authorize checks that the caller (the role the tenant middleware stored on
// ctx) may perform action. It returns FORBIDDEN when the role lacks the
// permission, and NOT_FOUND when ctx has no tenant at all, which means the
// call did not come through a /orgs/{orgId} route.
func Authorize(ctx context.Context, action Action) error {
	role, ok := RoleFrom(ctx)
	if !ok {
		return apperr.NotFound()
	}
	if !Can(role, action) {
		return apperr.Forbidden()
	}
	return nil
}

// Require is a route middleware that applies Authorize. It must run after Tenant.
func Require(action Action) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := Authorize(c.Request.Context(), action); err != nil {
			httpx.Error(c, err)
			return
		}
		c.Next()
	}
}
