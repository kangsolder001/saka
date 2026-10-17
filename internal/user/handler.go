package user

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/httpx"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

// ProfileService is what the handler needs to read and change profiles;
// *Service implements it.
type ProfileService interface {
	GetSummary(ctx context.Context, q db.DBTX, id uuid.UUID) (Summary, error)
	UpdateProfile(ctx context.Context, q db.DBTX, id uuid.UUID, in ProfileUpdate) (User, error)
}

var _ ProfileService = (*Service)(nil)

// Handler exposes the "my account" endpoints under /me (spec §6).
type Handler struct {
	profiles ProfileService
	pool     db.DBTX // for reads and single-statement writes outside a transaction
}

// NewHandler creates the /me handler.
func NewHandler(profiles ProfileService, pool db.DBTX) *Handler {
	return &Handler{profiles: profiles, pool: pool}
}

// Routes registers the /me endpoints under rg (expected to be /api/v1).
// authenticate must put the caller's user ID on the request context (see
// reqctx.UserID); the user module cannot import auth, so the composition
// root passes auth's middleware in.
func (h *Handler) Routes(rg *gin.RouterGroup, authenticate gin.HandlerFunc) {
	me := rg.Group("/me", authenticate)
	me.GET("", h.get)
	me.PATCH("", h.patch)
}

// currentUser returns the authenticated user's ID from the request context.
func currentUser(c *gin.Context) (uuid.UUID, bool) {
	id, ok := reqctx.UserID(c.Request.Context())
	if !ok {
		// Wiring bug: the route was mounted without the authenticate middleware.
		httpx.Error(c, apperr.Unauthenticated())
	}
	return id, ok
}

type profileResponse struct {
	ID            uuid.UUID `json:"id"`
	Email         string    `json:"email"`
	Name          string    `json:"name"`
	AvatarURL     *string   `json:"avatar_url"`
	EmailVerified bool      `json:"email_verified"`
}

func profileFromSummary(s Summary) profileResponse {
	return profileResponse{ID: s.ID, Email: s.Email, Name: s.Name, AvatarURL: s.AvatarURL, EmailVerified: s.EmailVerified()}
}

// get returns the caller's profile from the cached summary (spec §5.6).
func (h *Handler) get(c *gin.Context) {
	id, ok := currentUser(c)
	if !ok {
		return
	}
	sum, err := h.profiles.GetSummary(c.Request.Context(), h.pool, id)
	if err != nil {
		httpx.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"user": profileFromSummary(sum),
		// Filled in once organizations exist (Phase 4).
		"organizations": []any{},
	})
}

// optionalString tells a missing JSON field apart from an explicit null, which
// PATCH needs (null removes the avatar, absent leaves it alone).
type optionalString struct {
	Set   bool
	Value *string
}

func (o *optionalString) UnmarshalJSON(b []byte) error {
	o.Set = true
	return json.Unmarshal(b, &o.Value)
}

type patchRequest struct {
	Name      *string        `json:"name"`
	AvatarURL optionalString `json:"avatar_url"`
}

func (h *Handler) patch(c *gin.Context) {
	id, ok := currentUser(c)
	if !ok {
		return
	}
	var req patchRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	updated, err := h.profiles.UpdateProfile(c.Request.Context(), h.pool, id, ProfileUpdate{
		Name:      req.Name,
		SetAvatar: req.AvatarURL.Set,
		AvatarURL: req.AvatarURL.Value,
	})
	if err != nil {
		httpx.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": profileFromSummary(updated.Summary())})
}
