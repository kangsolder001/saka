//go:build integration

package app_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/app"
	"github.com/kangsolder001/saka/internal/billing/payment/fake"
	"github.com/kangsolder001/saka/internal/entitlement"
	"github.com/kangsolder001/saka/internal/platform/httpx"
)

// Checks behind the security review (spec §11.3), driven through the real router.

func TestSecurity_EveryResponseCarriesTheSecurityHeaders(t *testing.T) {
	s := newStack(t)

	for name, res := range map[string]response{
		"success":           s.get("/api/v1/plans", nil),
		"unknown route":     s.get("/api/v1/nope", nil),
		"unauthenticated":   s.get("/api/v1/me", nil),
		"validation error":  s.post(registerPath, `{}`),
		"health":            s.get("/healthz", nil),
		"webhook rejection": s.post(webhookPath, `{}`),
	} {
		h := res.Header()
		assert.Equal(t, "nosniff", h.Get("X-Content-Type-Options"), name)
		assert.Equal(t, "DENY", h.Get("X-Frame-Options"), name)
		assert.Equal(t, "no-referrer", h.Get("Referrer-Policy"), name)
		assert.Equal(t, "no-store", h.Get("Cache-Control"), name)
		assert.Contains(t, h.Get("Content-Security-Policy"), "default-src 'none'", name)
		assert.NotEmpty(t, h.Get(httpx.HeaderRequestID), name)
		assert.Empty(t, h.Get("Server"), name, "no server banner")
		assert.Empty(t, h.Get("X-Powered-By"), name)
	}
}

func TestSecurity_RequestBodiesAreBoundedAndStrict(t *testing.T) {
	s := newStack(t)
	huge := `{"email":"a@example.com","password":"` + strings.Repeat("x", httpx.MaxBodyBytes+1) + `"}`

	tooLarge := s.post(registerPath, huge)
	unknownField := s.post(registerPath, `{"email":"a@example.com","password":"correct-horse","name":"A","organization_name":"O","is_admin":true}`)
	trailing := s.post(registerPath, registerBody("a@example.com")+`{"x":1}`)
	notJSON := s.post(registerPath, `email=a@example.com`)

	for name, res := range map[string]response{"too large": tooLarge, "unknown field": unknownField, "trailing data": trailing, "not json": notJSON} {
		assert.Equal(t, http.StatusBadRequest, res.Code, name)
		assert.Equal(t, "VALIDATION_ERROR", errorOf(res)["code"], name)
	}
	assert.Zero(t, s.orgCount(), "nothing was created by the refused requests")

	// The webhook reads its own body: it is capped as well, before the signature.
	body, header := fake.Webhook(paid("evt-big", "INV-2026-000001", 99000))
	oversized := s.do(http.MethodPost, webhookPath, string(body)+strings.Repeat(" ", 1<<20+1), map[string]string{fake.SignatureHeader: header.Get(fake.SignatureHeader)})
	assert.Contains(t, []int{http.StatusBadRequest, http.StatusUnauthorized}, oversized.Code, "refused, never processed")
	assert.Zero(t, s.paymentEvents())
}

func TestSecurity_MalformedIdentifiersAreNotFoundNeverServerErrors(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam()
	for name, path := range map[string]string{
		"org id":        "/api/v1/orgs/not-a-uuid",
		"member id":     "/api/v1/orgs/" + tm.orgID + "/members/not-a-uuid",
		"invitation id": "/api/v1/orgs/" + tm.orgID + "/invitations/not-a-uuid",
		"invoice id":    "/api/v1/orgs/" + tm.orgID + "/invoices/not-a-uuid",
		"session id":    "/api/v1/me/sessions/not-a-uuid",
		"sql in id":     "/api/v1/orgs/" + tm.orgID + "/members/1%27%20OR%20%271%27=%271",
	} {
		method := http.MethodGet
		if name == "session id" {
			method = http.MethodDelete
		}
		res := s.do(method, path, "", bearer(tm.owner))

		assert.Equal(t, http.StatusNotFound, res.Code, name)
	}
	for name, query := range map[string]string{"limit text": "limit=abc", "limit huge": "limit=100000", "cursor garbage": "cursor=%00%00", "limit negative": "limit=-1"} {
		res := s.get("/api/v1/orgs?"+query, bearer(tm.owner))

		assert.Equal(t, http.StatusBadRequest, res.Code, name)
	}
}

func TestSecurity_AnUnexpectedErrorOrPanicNeverLeaksInternals(t *testing.T) {
	const secret = "pq: password authentication failed for user saka_app at 10.1.2.3"
	s := newStack(t, func(o *app.Options) {
		o.TenantRoutes = func(rg *gin.RouterGroup, _ *entitlement.Service) {
			rg.GET("/boom-error", func(c *gin.Context) { httpx.Error(c, errors.New(secret)) })
			rg.GET("/boom-panic", func(*gin.Context) { panic(secret) })
		}
	})
	tm := s.newTeam()

	for _, route := range []string{"boom-error", "boom-panic"} {
		res := s.get("/api/v1/orgs/"+tm.orgID+"/"+route, bearer(tm.owner))

		assert.Equal(t, http.StatusInternalServerError, res.Code, route)
		assert.Equal(t, "INTERNAL_ERROR", errorOf(res)["code"], route)
		assert.NotContains(t, res.Body.String(), "password authentication", route)
		assert.NotContains(t, res.Body.String(), "10.1.2.3", route)
		assert.NotContains(t, res.Body.String(), "goroutine", route, "no stack trace")
	}
	// The server keeps serving after a panic.
	assert.Equal(t, http.StatusOK, s.get("/healthz", nil).Code)
}

func TestSecurity_TheRefreshCookieIsHttpOnlyStrictAndScopedToTheAuthPath(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")

	res := s.post(loginPath, loginBody("ann@example.com", "correct-horse"))

	require.Equal(t, http.StatusOK, res.Code)
	setCookie := res.Header().Get("Set-Cookie")
	assert.Contains(t, setCookie, "HttpOnly")
	assert.Contains(t, setCookie, "SameSite=Strict")
	assert.Contains(t, setCookie, "Path=/api/v1/auth")
	assert.NotContains(t, res.Body.String(), "refresh_token", "with cookie delivery the refresh token is not in the body")
}
