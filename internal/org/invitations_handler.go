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

type invitationResponse struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Role      Role      `json:"role"`
	InvitedBy uuid.UUID `json:"invited_by"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

func invitationJSON(i Invitation) invitationResponse {
	return invitationResponse{ID: i.ID, Email: i.Email, Role: i.Role, InvitedBy: i.InvitedBy, ExpiresAt: i.ExpiresAt, CreatedAt: i.CreatedAt}
}

// invitationRoutes registers the per-organization invitation endpoints.
// verified guards the routes that send email: an unverified address must not
// be able to make the service mail strangers (spec §5.2).
func (h *Handler) invitationRoutes(one *gin.RouterGroup, verified gin.HandlerFunc) {
	one.GET("/invitations", h.listInvitations)
	one.POST("/invitations", verified, h.invite)
	one.DELETE("/invitations/:id", h.revokeInvitation)
	one.POST("/invitations/:id/resend", verified, h.resendInvitation)
}

func (h *Handler) listInvitations(c *gin.Context) {
	params, err := pagination.ParseQuery(c.Request.URL.Query())
	if err != nil {
		httpx.Error(c, err)
		return
	}
	rows, err := h.svc.ListInvitations(c.Request.Context(), params)
	if err != nil {
		httpx.Error(c, err)
		return
	}
	page := pagination.Paginate(rows, params.Limit, Invitation.CursorOf)
	items := make([]invitationResponse, len(page.Items))
	for i, inv := range page.Items {
		items[i] = invitationJSON(inv)
	}
	c.JSON(http.StatusOK, pagination.Page[invitationResponse]{Items: items, NextCursor: page.NextCursor})
}

type inviteRequest struct {
	Email string `json:"email" validate:"required,email,max=254"`
	Role  Role   `json:"role" validate:"required"`
}

// invite answers 201 for a new invitation and 200 when it refreshed the open
// one for the same address.
func (h *Handler) invite(c *gin.Context) {
	var req inviteRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	inv, refreshed, err := h.svc.Invite(c.Request.Context(), req.Email, req.Role)
	if err != nil {
		httpx.Error(c, err)
		return
	}
	status := http.StatusCreated
	if refreshed {
		status = http.StatusOK
	}
	c.JSON(status, gin.H{"invitation": invitationJSON(inv)})
}

func invitationID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Error(c, apperr.NotFound())
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) revokeInvitation(c *gin.Context) {
	id, ok := invitationID(c)
	if !ok {
		return
	}
	if err := h.svc.RevokeInvitation(c.Request.Context(), id); err != nil {
		httpx.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) resendInvitation(c *gin.Context) {
	id, ok := invitationID(c)
	if !ok {
		return
	}
	inv, err := h.svc.ResendInvitation(c.Request.Context(), id)
	if err != nil {
		httpx.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"invitation": invitationJSON(inv)})
}
