package org_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/kangsolder001/saka/internal/org"
	"github.com/kangsolder001/saka/internal/platform/apperr"
)

// matrix is spec §7.2 transcribed row by row (owner, admin, member).
var matrix = []struct {
	action org.Action
	owner  bool
	admin  bool
	member bool
}{
	{org.ActionView, true, true, true},
	{org.ActionUpdateSettings, true, true, false},
	{org.ActionInvite, true, true, false},
	{org.ActionChangeRole, true, true, false},
	{org.ActionRemoveMember, true, true, false},
	{org.ActionManageBilling, true, false, false},
	{org.ActionTransferOwnership, true, false, false},
	{org.ActionDeleteOrg, true, false, false},
	{org.ActionLeave, false, true, true},
}

func TestCan_MatchesThePermissionMatrixCellByCell(t *testing.T) {
	for _, row := range matrix {
		t.Run(string(row.action), func(t *testing.T) {
			assert.Equal(t, row.owner, org.Can(org.RoleOwner, row.action), "owner")
			assert.Equal(t, row.admin, org.Can(org.RoleAdmin, row.action), "admin")
			assert.Equal(t, row.member, org.Can(org.RoleMember, row.action), "member")
		})
	}
}

func TestCan_DeniesUnknownRolesAndActions(t *testing.T) {
	assert.False(t, org.Can("", org.ActionView))
	assert.False(t, org.Can("superadmin", org.ActionView))
	assert.False(t, org.Can(org.RoleOwner, "launch_missiles"))
}

func TestCanManageMember(t *testing.T) {
	tests := []struct {
		actor, target org.Role
		want          bool
	}{
		{org.RoleOwner, org.RoleAdmin, true},
		{org.RoleOwner, org.RoleMember, true},
		{org.RoleOwner, org.RoleOwner, false}, // ownership only moves by transfer
		{org.RoleAdmin, org.RoleMember, true},
		{org.RoleAdmin, org.RoleAdmin, false}, // spec §7.2 note *
		{org.RoleAdmin, org.RoleOwner, false},
		{org.RoleMember, org.RoleMember, false},
		{org.RoleMember, org.RoleAdmin, false},
		{org.RoleMember, org.RoleOwner, false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, org.CanManageMember(tt.actor, tt.target), "%s managing %s", tt.actor, tt.target)
	}
}

func TestAssignable_OwnerIsNeverAssignable(t *testing.T) {
	assert.True(t, org.Assignable(org.RoleAdmin))
	assert.True(t, org.Assignable(org.RoleMember))
	assert.False(t, org.Assignable(org.RoleOwner))
	assert.False(t, org.Assignable("root"))
}

func TestAuthorize(t *testing.T) {
	t.Run("allowed", func(t *testing.T) {
		ctx := org.WithRole(context.Background(), org.RoleAdmin)

		assert.NoError(t, org.Authorize(ctx, org.ActionInvite))
	})
	t.Run("insufficient role is FORBIDDEN", func(t *testing.T) {
		ctx := org.WithRole(context.Background(), org.RoleMember)

		err := org.Authorize(ctx, org.ActionInvite)

		assert.True(t, apperr.IsCode(err, apperr.CodeForbidden))
	})
	t.Run("no tenant on the context is NOT_FOUND", func(t *testing.T) {
		err := org.Authorize(context.Background(), org.ActionView)

		assert.True(t, apperr.IsCode(err, apperr.CodeNotFound))
	})
}

func TestRequire_BlocksRolesWithoutThePermission(t *testing.T) {
	setRole := func(role org.Role) gin.HandlerFunc {
		return func(c *gin.Context) {
			c.Request = c.Request.WithContext(org.WithRole(c.Request.Context(), role))
		}
	}
	for _, tt := range []struct {
		role org.Role
		want int
	}{
		{org.RoleOwner, http.StatusNoContent},
		{org.RoleAdmin, http.StatusForbidden},
		{org.RoleMember, http.StatusForbidden},
	} {
		t.Run(string(tt.role), func(t *testing.T) {
			r := gin.New()
			ran := false
			r.GET("/billing", setRole(tt.role), org.Require(org.ActionManageBilling), func(c *gin.Context) {
				ran = true
				c.Status(http.StatusNoContent)
			})

			rec := get(r, "/billing")

			assert.Equal(t, tt.want, rec.Code)
			assert.Equal(t, tt.want == http.StatusNoContent, ran, "the handler runs only when allowed")
		})
	}
}
