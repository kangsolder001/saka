package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kangsolder001/saka/internal/platform/httpx"
	"github.com/kangsolder001/saka/internal/user"
)

// Handler exposes the auth use cases over HTTP (spec §5.1).
type Handler struct {
	svc *Service
}

// NewHandler creates the HTTP handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Routes registers the auth endpoints under rg (expected to be /api/v1).
func (h *Handler) Routes(rg *gin.RouterGroup) {
	g := rg.Group("/auth")
	g.POST("/register", h.register)
	g.POST("/verify-email", h.verifyEmail)
}

type userResponse struct {
	ID            uuid.UUID `json:"id"`
	Email         string    `json:"email"`
	Name          string    `json:"name"`
	EmailVerified bool      `json:"email_verified"`
}

func toUserResponse(u user.User) userResponse {
	return userResponse{ID: u.ID, Email: u.Email, Name: u.Name, EmailVerified: u.EmailVerified()}
}

// registerRequest mirrors RegisterInput field for field (checked by the
// conversion in register), adding the JSON names and validation rules.
type registerRequest struct {
	Email            string `json:"email" validate:"required,email,max=254"`
	Password         string `json:"password" validate:"required"`
	Name             string `json:"name" validate:"required,max=100"`
	OrganizationName string `json:"organization_name" validate:"required,max=100"`
}

func (h *Handler) register(c *gin.Context) {
	var req registerRequest
	if !httpx.BindJSON(c, &req) {
		return
	}

	u, err := h.svc.Register(c.Request.Context(), RegisterInput(req))
	if err != nil {
		httpx.Error(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"user": toUserResponse(u)})
}

type verifyEmailRequest struct {
	Token string `json:"token" validate:"required,max=256"`
}

func (h *Handler) verifyEmail(c *gin.Context) {
	var req verifyEmailRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	if err := h.svc.VerifyEmail(c.Request.Context(), req.Token); err != nil {
		httpx.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"email_verified": true})
}
