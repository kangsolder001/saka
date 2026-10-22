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

// publicInvitationRoutes registers the endpoints addressed by the link in the
// invitation email. Looking up and declining need no login: the link's token is
// the credential, and the invitee may not have an account yet. Accepting needs
// a verified login, because it grants membership to whoever holds that email
// address (spec §7.4).
func (h *Handler) publicInvitationRoutes(rg *gin.RouterGroup, authenticate, requireVerifiedEmail gin.HandlerFunc) {
	rg.GET("/invitations/:token", h.invitationInfo)
	rg.POST("/invitations/:token/accept", authenticate, requireVerifiedEmail, h.acceptInvitation)
	rg.POST("/invitations/:token/decline", h.declineInvitation)
}

type invitationInfoResponse struct {
	Email         string    `json:"email"`
	Role          Role      `json:"role"`
	Organization  orgBrief  `json:"organization"`
	InviterName   string    `json:"inviter_name"`
	ExpiresAt     time.Time `json:"expires_at"`
	AccountExists bool      `json:"account_exists"`
}

type orgBrief struct {
	Name    string  `json:"name"`
	LogoURL *string `json:"logo_url"`
}

func (h *Handler) invitationInfo(c *gin.Context) {
	info, err := h.svc.InvitationByToken(c.Request.Context(), c.Param("token"))
	if err != nil {
		httpx.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"invitation": invitationInfoResponse{
		Email: info.Email, Role: info.Role, Organization: orgBrief{Name: info.OrganizationName, LogoURL: info.OrganizationLogo},
		InviterName: info.InviterName, ExpiresAt: info.ExpiresAt, AccountExists: info.AccountExists,
	}})
}

func (h *Handler) acceptInvitation(c *gin.Context) {
	userID, ok := currentUser(c)
	if !ok {
		return
	}
	joined, err := h.svc.AcceptInvitation(c.Request.Context(), userID, c.Param("token"))
	if err != nil {
		httpx.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"organization": memberOrganizationResponse{
		organizationResponse: organizationJSON(joined.Organization), Role: joined.Role,
	}})
}

func (h *Handler) declineInvitation(c *gin.Context) {
	if err := h.svc.DeclineInvitation(c.Request.Context(), c.Param("token")); err != nil {
		httpx.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
