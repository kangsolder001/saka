package auth

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/httpx"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

const principalKey = "auth.principal"

// Middleware provides the gin middleware that protects routes. Other modules
// receive it from the composition root and attach it to their route groups.
type Middleware struct {
	svc *Service
}

// NewMiddleware creates the middleware set.
func NewMiddleware(svc *Service) *Middleware {
	return &Middleware{svc: svc}
}

// Authenticate requires a valid "Authorization: Bearer <access token>". It
// stores the Principal on the gin context (see PrincipalFrom) and the user
// and session IDs on the request context, so logs and services see them.
func (m *Middleware) Authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok {
			httpx.Error(c, apperr.Unauthenticated())
			return
		}

		p, err := m.svc.Authenticate(c.Request.Context(), token)
		if err != nil {
			httpx.Error(c, err)
			return
		}

		ctx := reqctx.WithUserID(c.Request.Context(), p.UserID)
		ctx = reqctx.WithSessionID(ctx, p.SessionID)
		c.Request = c.Request.WithContext(ctx)
		c.Set(principalKey, p)
		c.Next()
	}
}

// RequireVerifiedEmail blocks callers whose email is not verified with
// EMAIL_NOT_VERIFIED (inviting members, checkout, ...). It must run after
// Authenticate.
func (m *Middleware) RequireVerifiedEmail() gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := PrincipalFrom(c)
		if !ok {
			// Wiring bug: the guard was mounted without Authenticate in front.
			httpx.Error(c, apperr.Unauthenticated())
			return
		}
		if !p.EmailVerified {
			httpx.Error(c, apperr.EmailNotVerified())
			return
		}
		c.Next()
	}
}

// PrincipalFrom returns the authenticated caller stored by Authenticate.
func PrincipalFrom(c *gin.Context) (Principal, bool) {
	v, ok := c.Get(principalKey)
	if !ok {
		return Principal{}, false
	}
	p, ok := v.(Principal)
	return p, ok
}

// bearerToken extracts the token from "Bearer <token>" (scheme is case-insensitive).
func bearerToken(header string) (string, bool) {
	scheme, token, found := strings.Cut(strings.TrimSpace(header), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsAny(token, " \t") {
		return "", false
	}
	return token, true
}
