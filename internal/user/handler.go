package user

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

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

// AccountSecurity is what the handler needs for credentials and sessions.
// auth owns passwords, one-time tokens and sessions and already depends on
// this package, so the user module declares the interface and auth.Service
// implements it (the composition root connects them).
type AccountSecurity interface {
	// ChangePassword checks the current password, sets the new one and revokes
	// every session except currentSession.
	ChangePassword(ctx context.Context, userID, currentSession uuid.UUID, currentPassword, newPassword string) error
	// RequestEmailChange checks the current password and emails a confirmation
	// link to newEmail; nothing changes until the link is used.
	RequestEmailChange(ctx context.Context, userID uuid.UUID, currentPassword, newEmail string) error
	// ConfirmEmailChange applies a requested change from the token in that link
	// and returns the new email.
	ConfirmEmailChange(ctx context.Context, rawToken string) (newEmail string, err error)
	// ListSessions returns the user's live sessions, flagging currentSession.
	ListSessions(ctx context.Context, userID, currentSession uuid.UUID) ([]Session, error)
	// RevokeSession ends one of the user's sessions; NOT_FOUND when it is not theirs.
	RevokeSession(ctx context.Context, userID, sessionID uuid.UUID) error
}

// Session is one live login of a user (a refresh token family).
type Session struct {
	ID         uuid.UUID
	UserAgent  string
	IP         string
	StartedAt  time.Time
	LastUsedAt time.Time
	ExpiresAt  time.Time
	// Current marks the session that made the request.
	Current bool
}

var _ ProfileService = (*Service)(nil)

// Dependencies are the collaborators of Handler.
type Dependencies struct {
	Profiles ProfileService
	Security AccountSecurity
	// Pool serves reads and single-statement writes outside a transaction.
	Pool db.DBTX
}

// Handler exposes the "my account" endpoints under /me (spec §6).
type Handler struct {
	Dependencies
}

// NewHandler creates the /me handler.
func NewHandler(deps Dependencies) *Handler {
	return &Handler{Dependencies: deps}
}

// Routes registers the /me endpoints under rg (expected to be /api/v1).
// authenticate must put the caller's user ID on the request context (see
// reqctx.UserID); the user module cannot import auth, so the composition
// root passes auth's middleware in.
func (h *Handler) Routes(rg *gin.RouterGroup, authenticate gin.HandlerFunc) {
	me := rg.Group("/me", authenticate)
	me.GET("", h.get)
	me.PATCH("", h.patch)
	me.POST("/password", h.changePassword)
	me.POST("/email", h.requestEmailChange)
	me.GET("/sessions", h.listSessions)
	me.DELETE("/sessions/:id", h.revokeSession)

	// The link in the confirmation email is opened from the new inbox, maybe on
	// another device, so the one-time token is the credential (like verify-email).
	rg.POST("/me/email/confirm", h.confirmEmailChange)
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
	sum, err := h.Profiles.GetSummary(c.Request.Context(), h.Pool, id)
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
	updated, err := h.Profiles.UpdateProfile(c.Request.Context(), h.Pool, id, ProfileUpdate{
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

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password" validate:"required,max=1024"` // capped so a huge body cannot force a huge hash
	NewPassword     string `json:"new_password" validate:"required,max=1024"`
}

// changePassword keeps the caller's session (taken from the access token) and
// ends every other one.
func (h *Handler) changePassword(c *gin.Context) {
	id, ok := currentUser(c)
	if !ok {
		return
	}
	session, ok := reqctx.SessionID(c.Request.Context())
	if !ok {
		httpx.Error(c, apperr.Unauthenticated()) // wiring bug, as in currentUser
		return
	}
	var req changePasswordRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	if err := h.Security.ChangePassword(c.Request.Context(), id, session, req.CurrentPassword, req.NewPassword); err != nil {
		httpx.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Password berhasil diubah. Sesi di perangkat lain telah diakhiri."})
}

type requestEmailChangeRequest struct {
	NewEmail        string `json:"new_email" validate:"required,email,max=254"`
	CurrentPassword string `json:"current_password" validate:"required,max=1024"`
}

func (h *Handler) requestEmailChange(c *gin.Context) {
	id, ok := currentUser(c)
	if !ok {
		return
	}
	var req requestEmailChangeRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	if err := h.Security.RequestEmailChange(c.Request.Context(), id, req.CurrentPassword, req.NewEmail); err != nil {
		httpx.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Kami telah mengirim tautan konfirmasi ke email baru Anda. Email akun belum berubah sampai tautan itu dibuka."})
}

type confirmEmailChangeRequest struct {
	Token string `json:"token" validate:"required,max=256"`
}

func (h *Handler) confirmEmailChange(c *gin.Context) {
	var req confirmEmailChangeRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	newEmail, err := h.Security.ConfirmEmailChange(c.Request.Context(), req.Token)
	if err != nil {
		httpx.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"email": newEmail, "email_verified": true})
}

type sessionResponse struct {
	ID         uuid.UUID `json:"id"`
	UserAgent  string    `json:"user_agent"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Current    bool      `json:"current"`
}

func (h *Handler) listSessions(c *gin.Context) {
	id, ok := currentUser(c)
	if !ok {
		return
	}
	current, _ := reqctx.SessionID(c.Request.Context())
	sessions, err := h.Security.ListSessions(c.Request.Context(), id, current)
	if err != nil {
		httpx.Error(c, err)
		return
	}
	out := make([]sessionResponse, len(sessions))
	for i, s := range sessions {
		out[i] = sessionResponse{
			ID: s.ID, UserAgent: s.UserAgent, IP: s.IP,
			CreatedAt: s.StartedAt, LastUsedAt: s.LastUsedAt, ExpiresAt: s.ExpiresAt, Current: s.Current,
		}
	}
	c.JSON(http.StatusOK, gin.H{"sessions": out})
}

func (h *Handler) revokeSession(c *gin.Context) {
	id, ok := currentUser(c)
	if !ok {
		return
	}
	sessionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Error(c, apperr.NotFound()) // same answer as an unknown session
		return
	}
	if err := h.Security.RevokeSession(c.Request.Context(), id, sessionID); err != nil {
		httpx.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
