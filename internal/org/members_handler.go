package org

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/httpx"
	"github.com/kangsolder001/saka/internal/platform/pagination"
)

type memberResponse struct {
	UserID    uuid.UUID `json:"user_id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	AvatarURL *string   `json:"avatar_url"`
	Role      Role      `json:"role"`
	JoinedAt  time.Time `json:"joined_at"`
}

func memberJSON(m Member) memberResponse {
	return memberResponse{UserID: m.UserID, Email: m.Email, Name: m.Name, AvatarURL: m.AvatarURL, Role: m.Role, JoinedAt: m.JoinedAt}
}

// memberRoutes registers the member endpoints on the tenant group.
func (h *Handler) memberRoutes(one *gin.RouterGroup) {
	one.GET("/members", h.listMembers)
	one.PATCH("/members/:userId", h.changeRole)
	one.DELETE("/members/:userId", h.removeMember)
	one.POST("/leave", h.leave)
	one.POST("/transfer-ownership", h.transferOwnership)
}

func (h *Handler) listMembers(c *gin.Context) {
	params, err := pagination.ParseQuery(c.Request.URL.Query())
	if err != nil {
		httpx.Error(c, err)
		return
	}
	rows, err := h.svc.ListMembers(c.Request.Context(), params)
	if err != nil {
		httpx.Error(c, err)
		return
	}
	page := pagination.Paginate(rows, params.Limit, Member.CursorOf)
	items := make([]memberResponse, len(page.Items))
	for i, m := range page.Items {
		items[i] = memberJSON(m)
	}
	c.JSON(http.StatusOK, pagination.Page[memberResponse]{Items: items, NextCursor: page.NextCursor})
}

// targetUser reads the :userId parameter. A malformed ID is the same answer as
// an unknown member.
func targetUser(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		httpx.Error(c, apperr.NotFound())
		return uuid.Nil, false
	}
	return id, true
}

type changeRoleRequest struct {
	Role Role `json:"role" validate:"required"`
}

func (h *Handler) changeRole(c *gin.Context) {
	target, ok := targetUser(c)
	if !ok {
		return
	}
	var req changeRoleRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	changed, err := h.svc.ChangeRole(c.Request.Context(), target, req.Role)
	if err != nil {
		httpx.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"member": memberJSON(changed)})
}

func (h *Handler) removeMember(c *gin.Context) {
	target, ok := targetUser(c)
	if !ok {
		return
	}
	if err := h.svc.RemoveMember(c.Request.Context(), target); err != nil {
		httpx.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) leave(c *gin.Context) {
	if err := h.svc.Leave(c.Request.Context()); err != nil {
		httpx.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type transferRequest struct {
	UserID          uuid.UUID `json:"user_id" validate:"required"`
	CurrentPassword string    `json:"current_password" validate:"required,max=1024"` // capped so a huge body cannot force a huge hash
}

func (h *Handler) transferOwnership(c *gin.Context) {
	var req transferRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	if err := h.svc.TransferOwnership(c.Request.Context(), req.UserID, req.CurrentPassword); err != nil {
		httpx.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
