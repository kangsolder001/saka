package auth

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/httpx"
	"github.com/kangsolder001/saka/internal/user"
)

const (
	// RefreshCookieName is the cookie carrying the refresh token for web clients.
	RefreshCookieName = "refresh_token"
	refreshCookiePath = "/api/v1/auth"

	deliveryCookie = "cookie"
	deliveryBody   = "body"
)

// HandlerConfig holds HTTP-level settings.
type HandlerConfig struct {
	// SecureCookie sets the Secure flag on the refresh cookie. Enable it
	// wherever the API is served over HTTPS (always in production).
	SecureCookie bool
}

// Handler exposes the auth use cases over HTTP (spec §5.1).
type Handler struct {
	svc *Service
	mw  *Middleware
	cfg HandlerConfig
}

// NewHandler creates the HTTP handler. It uses mw to protect the endpoints
// that need a logged-in user.
func NewHandler(svc *Service, mw *Middleware, cfg HandlerConfig) *Handler {
	return &Handler{svc: svc, mw: mw, cfg: cfg}
}

// Routes registers the auth endpoints under rg (expected to be /api/v1).
func (h *Handler) Routes(rg *gin.RouterGroup) {
	g := rg.Group("/auth")
	g.POST("/register", h.register)
	g.POST("/verify-email", h.verifyEmail)
	g.POST("/login", h.login)
	g.POST("/refresh", h.refresh)
	g.POST("/forgot-password", h.forgotPassword)
	g.POST("/reset-password", h.resetPassword)

	authed := g.Group("", h.mw.Authenticate())
	authed.POST("/resend-verification", h.resendVerification)
	authed.POST("/logout", h.logout)
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

type loginRequest struct {
	Email    string `json:"email" validate:"required,email,max=254"`
	Password string `json:"password" validate:"required,max=1024"` // capped so a huge body cannot force a huge hash
	// TokenDelivery chooses how the refresh token reaches the client:
	// "cookie" (default, for browsers) or "body" (for mobile/API clients).
	TokenDelivery string `json:"token_delivery" validate:"omitempty,oneof=cookie body"`
}

type tokenResponse struct {
	AccessToken  string        `json:"access_token"`
	TokenType    string        `json:"token_type"`
	ExpiresIn    int           `json:"expires_in"`
	RefreshToken string        `json:"refresh_token,omitempty"`
	User         *userResponse `json:"user,omitempty"`
}

func (h *Handler) login(c *gin.Context) {
	var req loginRequest
	if !httpx.BindJSON(c, &req) {
		return
	}

	res, err := h.svc.Login(c.Request.Context(), LoginInput{
		Email:    req.Email,
		Password: req.Password,
		Client:   clientInfo(c),
	})
	if err != nil {
		httpx.Error(c, err)
		return
	}

	u := toUserResponse(res.User)
	h.writeTokens(c, req.TokenDelivery, res.AccessToken, res.AccessExpiresAt, res.RefreshToken, res.RefreshExpiresAt, &u)
}

// writeTokens sends the access token in the body and the refresh token either
// as an HttpOnly cookie (default) or in the body.
func (h *Handler) writeTokens(c *gin.Context, delivery, access string, accessExpires time.Time, refresh string, refreshExpires time.Time, u *userResponse) {
	body := tokenResponse{
		AccessToken: access,
		TokenType:   "Bearer",
		ExpiresIn:   int(accessExpires.Sub(h.svc.Clock.Now()).Seconds()),
		User:        u,
	}
	if delivery == deliveryBody {
		body.RefreshToken = refresh
	} else {
		h.setRefreshCookie(c, refresh, refreshExpires)
	}
	c.JSON(http.StatusOK, body)
}

func (h *Handler) setRefreshCookie(c *gin.Context, value string, expires time.Time) {
	maxAge := int(expires.Sub(h.svc.Clock.Now()).Seconds())
	//nolint:gosec // G124: Secure follows config on purpose (on in production, off for http://localhost development)
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     RefreshCookieName,
		Value:    value,
		Path:     refreshCookiePath,
		Expires:  expires,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   h.cfg.SecureCookie,
		SameSite: http.SameSiteStrictMode,
	})
}

func clientInfo(c *gin.Context) ClientInfo {
	return ClientInfo{UserAgent: c.Request.UserAgent(), IP: c.ClientIP()}
}

func (h *Handler) resendVerification(c *gin.Context) {
	p, _ := PrincipalFrom(c) // Authenticate guarantees it
	if err := h.svc.ResendVerification(c.Request.Context(), p.UserID); err != nil {
		httpx.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "sent"})
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required,max=256"`
}

// refresh rotates the refresh token. The token comes from the HttpOnly cookie
// (web) or from the JSON body (mobile); the new one is returned the same way.
func (h *Handler) refresh(c *gin.Context) {
	raw, fromCookie := "", false
	if cookie, err := c.Cookie(RefreshCookieName); err == nil && cookie != "" {
		raw, fromCookie = cookie, true
	} else {
		var req refreshRequest
		if !httpx.BindJSON(c, &req) {
			return
		}
		raw = req.RefreshToken
	}

	res, err := h.svc.Refresh(c.Request.Context(), raw, clientInfo(c))
	if err != nil {
		if fromCookie && apperr.IsCode(err, apperr.CodeUnauthenticated) {
			h.clearRefreshCookie(c) // the browser's cookie is dead: drop it
		}
		httpx.Error(c, err)
		return
	}

	delivery := deliveryBody
	if fromCookie {
		delivery = deliveryCookie
	}
	h.writeTokens(c, delivery, res.AccessToken, res.AccessExpiresAt, res.RefreshToken, res.RefreshExpiresAt, nil)
}

func (h *Handler) clearRefreshCookie(c *gin.Context) {
	//nolint:gosec // G124: Secure follows config on purpose (on in production, off for http://localhost development)
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     RefreshCookieName,
		Value:    "",
		Path:     refreshCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.cfg.SecureCookie,
		SameSite: http.SameSiteStrictMode,
	})
}

func (h *Handler) logout(c *gin.Context) {
	p, _ := PrincipalFrom(c) // Authenticate guarantees it
	if err := h.svc.Logout(c.Request.Context(), p); err != nil {
		httpx.Error(c, err)
		return
	}
	h.clearRefreshCookie(c)
	c.Status(http.StatusNoContent)
}

type forgotPasswordRequest struct {
	Email string `json:"email" validate:"required,email,max=254"`
}

// forgotPassword always answers 200 with the same body, whether or not the
// email is registered (spec §5.2).
func (h *Handler) forgotPassword(c *gin.Context) {
	var req forgotPasswordRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	if err := h.svc.ForgotPassword(c.Request.Context(), req.Email); err != nil {
		httpx.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Jika email terdaftar, tautan untuk mengatur ulang password telah dikirim."})
}

type resetPasswordRequest struct {
	Token    string `json:"token" validate:"required,max=256"`
	Password string `json:"password" validate:"required,max=1024"`
}

func (h *Handler) resetPassword(c *gin.Context) {
	var req resetPasswordRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	if err := h.svc.ResetPassword(c.Request.Context(), req.Token, req.Password); err != nil {
		httpx.Error(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Password berhasil diubah. Silakan login kembali."})
}
