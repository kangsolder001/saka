package org

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/httpx"
	"github.com/kangsolder001/saka/internal/platform/pagination"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

// TenantRoutes returns the route group /orgs/:orgId for resources that belong
// to one organization. Every route in it has already passed authentication and
// the tenant middleware, so handlers can rely on reqctx.OrgID and RoleFrom.
// Other modules (billing, entitlement, future domain modules) mount their
// per-organization routes on it from the composition root.
func TenantRoutes(rg *gin.RouterGroup, authenticate gin.HandlerFunc, members MembershipReader) *gin.RouterGroup {
	return rg.Group("/orgs/:"+OrgIDParam, authenticate, Tenant(members))
}

// Handler exposes the organization endpoints (spec §7.1).
type Handler struct {
	svc *Service
	// createLimit runs before POST /orgs (after authentication); nil means none.
	createLimit gin.HandlerFunc
}

// NewHandler creates the organization handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// WithCreateLimit makes POST /orgs run mw after authentication, for example a
// per-user rate limit. It returns the handler for chaining.
func (h *Handler) WithCreateLimit(mw gin.HandlerFunc) *Handler {
	h.createLimit = mw
	return h
}

// Routes registers the organization endpoints under rg (expected to be
// /api/v1). authenticate must put the caller's user ID on the request context;
// requireVerifiedEmail rejects callers whose email is not verified, and guards
// the routes that make the service send email.
func (h *Handler) Routes(rg *gin.RouterGroup, authenticate, requireVerifiedEmail gin.HandlerFunc) {
	orgs := rg.Group("/orgs", authenticate)
	orgs.GET("", h.list)
	if h.createLimit != nil {
		orgs.POST("", h.createLimit, h.create)
	} else {
		orgs.POST("", h.create)
	}

	one := TenantRoutes(rg, authenticate, h.svc)
	one.GET("", h.get)
	one.PATCH("", h.update)
	one.DELETE("", h.delete)
	h.memberRoutes(one)
	h.invitationRoutes(one, requireVerifiedEmail)
	h.publicInvitationRoutes(rg, authenticate, requireVerifiedEmail)
}

type organizationResponse struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	LogoURL   *string   `json:"logo_url"`
	OwnerID   uuid.UUID `json:"owner_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// organizationJSON is a plain conversion: if Organization gains a field, this
// stops compiling until the response type decides whether to expose it.
func organizationJSON(o Organization) organizationResponse { return organizationResponse(o) }

// memberOrganizationResponse adds the caller's role to an organization.
type memberOrganizationResponse struct {
	organizationResponse
	Role Role `json:"role"`
}

func currentUser(c *gin.Context) (uuid.UUID, bool) {
	id, ok := reqctx.UserID(c.Request.Context())
	if !ok {
		// Wiring bug: the route was mounted without the authenticate middleware.
		httpx.Error(c, apperr.Unauthenticated())
	}
	return id, ok
}

func (h *Handler) list(c *gin.Context) {
	userID, ok := currentUser(c)
	if !ok {
		return
	}
	params, err := pagination.ParseQuery(c.Request.URL.Query())
	if err != nil {
		httpx.Error(c, err)
		return
	}
	rows, err := h.svc.ListForUser(c.Request.Context(), userID, params)
	if err != nil {
		httpx.Error(c, err)
		return
	}
	page := pagination.Paginate(rows, params.Limit, MyOrganization.CursorOf)
	items := make([]memberOrganizationResponse, len(page.Items))
	for i, m := range page.Items {
		items[i] = memberOrganizationResponse{organizationResponse: organizationJSON(m.Organization), Role: m.Role}
	}
	c.JSON(http.StatusOK, pagination.Page[memberOrganizationResponse]{Items: items, NextCursor: page.NextCursor})
}

type createRequest struct {
	Name string `json:"name" validate:"required,max=200"`
	Slug string `json:"slug" validate:"omitempty,max=100"`
}

func (h *Handler) create(c *gin.Context) {
	userID, ok := currentUser(c)
	if !ok {
		return
	}
	var req createRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	created, err := h.svc.Create(c.Request.Context(), userID, req.Name, req.Slug)
	if err != nil {
		httpx.Error(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"organization": memberOrganizationResponse{
		organizationResponse: organizationJSON(created), Role: RoleOwner,
	}})
}

func (h *Handler) get(c *gin.Context) {
	found, err := h.svc.Get(c.Request.Context())
	if err != nil {
		httpx.Error(c, err)
		return
	}
	role, _ := RoleFrom(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{"organization": memberOrganizationResponse{organizationResponse: organizationJSON(found), Role: role}})
}

type updateRequest struct {
	Name    *string                `json:"name" validate:"omitempty,max=200"`
	Slug    *string                `json:"slug" validate:"omitempty,max=100"`
	LogoURL httpx.Optional[string] `json:"logo_url"`
}

func (h *Handler) update(c *gin.Context) {
	var req updateRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	updated, err := h.svc.Update(c.Request.Context(), Update{
		Name: req.Name, Slug: req.Slug, SetLogo: req.LogoURL.Set, LogoURL: req.LogoURL.Value,
	})
	if err != nil {
		httpx.Error(c, err)
		return
	}
	role, _ := RoleFrom(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{"organization": memberOrganizationResponse{organizationResponse: organizationJSON(updated), Role: role}})
}

func (h *Handler) delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context()); err != nil {
		httpx.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
