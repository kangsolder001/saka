package httpx_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/httpx"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

func init() { gin.SetMode(gin.TestMode) }

const validTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

func newEngine(t *testing.T, production bool, origins ...string) (*gin.Engine, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	log, err := logger.New(buf, "debug")
	require.NoError(t, err)

	r := gin.New()
	r.Use(
		httpx.RequestContext(),
		httpx.AccessLog(log, clock.NewFake(time.Now())),
		httpx.Recover(log),
		httpx.SecurityHeaders(production),
		httpx.CORS(origins),
	)
	r.NoRoute(httpx.NotFound)
	return r, buf
}

func do(r http.Handler, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, target, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &m), line)
		out = append(out, m)
	}
	return out
}

func TestRequestContext_GeneratesIDsAndExposesThem(t *testing.T) {
	r, _ := newEngine(t, false)
	var seenTrace, seenRequest string
	r.GET("/x", func(c *gin.Context) {
		seenTrace = reqctx.TraceID(c.Request.Context())
		seenRequest = reqctx.RequestID(c.Request.Context())
		c.Status(http.StatusNoContent)
	})

	rec := do(r, http.MethodGet, "/x", nil)

	assert.Regexp(t, `^[0-9a-f]{32}$`, rec.Header().Get(httpx.HeaderTraceID))
	assert.NotEmpty(t, rec.Header().Get(httpx.HeaderRequestID))
	assert.Equal(t, rec.Header().Get(httpx.HeaderTraceID), seenTrace)
	assert.Equal(t, rec.Header().Get(httpx.HeaderRequestID), seenRequest)
}

func TestRequestContext_ContinuesIncomingTrace(t *testing.T) {
	r, _ := newEngine(t, false)
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	rec := do(r, http.MethodGet, "/x", map[string]string{httpx.HeaderTraceparent: validTraceparent})

	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", rec.Header().Get(httpx.HeaderTraceID))
}

func TestRequestContext_IgnoresMalformedTraceparentAndRequestIDHeader(t *testing.T) {
	r, _ := newEngine(t, false)
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	rec := do(r, http.MethodGet, "/x", map[string]string{
		httpx.HeaderTraceparent: "garbage",
		httpx.HeaderRequestID:   "client-chosen",
	})

	assert.Regexp(t, `^[0-9a-f]{32}$`, rec.Header().Get(httpx.HeaderTraceID))
	assert.NotEqual(t, "client-chosen", rec.Header().Get(httpx.HeaderRequestID))
}

func TestAccessLog_IncludesIDsAndRouteTemplateNotRawURL(t *testing.T) {
	r, buf := newEngine(t, false)
	r.GET("/items/:id", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	rec := do(r, http.MethodGet, "/items/secret-token-123?token=abc", map[string]string{httpx.HeaderTraceparent: validTraceparent})

	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, buf.String(), "secret-token-123")
	assert.NotContains(t, buf.String(), "token=abc")
	lines := logLines(t, buf)
	require.Len(t, lines, 1)
	line := lines[0]
	assert.Equal(t, "/items/:id", line["route"])
	assert.Equal(t, "GET", line["method"])
	assert.EqualValues(t, 200, line["status"])
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", line["trace_id"])
	assert.Equal(t, rec.Header().Get(httpx.HeaderRequestID), line["request_id"])
	assert.Equal(t, "info", line["level"])
	assert.Contains(t, line, "duration_ms")
}

func TestAccessLog_LevelsFollowStatus(t *testing.T) {
	r, buf := newEngine(t, false)
	r.GET("/bad", func(c *gin.Context) { httpx.Error(c, apperr.Validation(nil)) })
	r.GET("/boom", func(c *gin.Context) { httpx.Error(c, errors.New("db down")) })

	do(r, http.MethodGet, "/bad", nil)
	do(r, http.MethodGet, "/boom", nil)
	do(r, http.MethodGet, "/missing", nil)

	lines := logLines(t, buf)
	require.Len(t, lines, 3)
	assert.Equal(t, "warn", lines[0]["level"])
	assert.Equal(t, "error", lines[1]["level"])
	assert.Contains(t, lines[1]["error"], "db down", "internal cause must be logged")
	assert.Equal(t, "warn", lines[2]["level"])
	assert.Equal(t, "unmatched", lines[2]["route"])
}

func TestRecover_ReturnsStandardInternalErrorAndLogsPanic(t *testing.T) {
	r, buf := newEngine(t, false)
	r.GET("/panic", func(c *gin.Context) { panic("secret internal detail") })

	rec := do(r, http.MethodGet, "/panic", map[string]string{httpx.HeaderTraceparent: validTraceparent})

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"INTERNAL_ERROR","message":"Terjadi kesalahan pada server.","details":{}}}`, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "secret")
	assert.Contains(t, buf.String(), "secret internal detail")
	assert.Contains(t, buf.String(), `"message":"handler panicked"`)
}

func TestError_WritesStandardFormat(t *testing.T) {
	r, _ := newEngine(t, false)
	r.GET("/limit", func(c *gin.Context) { httpx.Error(c, apperr.PlanLimitReached("members", 3)) })

	rec := do(r, http.MethodGet, "/limit", nil)

	assert.Equal(t, http.StatusPaymentRequired, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"PLAN_LIMIT_REACHED","message":"Batas paket tercapai.","details":{"limit":"members","max":3}}}`, rec.Body.String())
}

func TestNotFound_UsesStandardFormat(t *testing.T) {
	r, _ := newEngine(t, false)

	rec := do(r, http.MethodGet, "/nope", nil)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"NOT_FOUND"`)
}

func TestSecurityHeaders(t *testing.T) {
	for _, production := range []bool{false, true} {
		r, _ := newEngine(t, production)
		r.GET("/x", func(c *gin.Context) { c.Status(http.StatusNoContent) })

		h := do(r, http.MethodGet, "/x", nil).Header()

		assert.Equal(t, "nosniff", h.Get("X-Content-Type-Options"))
		assert.Equal(t, "DENY", h.Get("X-Frame-Options"))
		assert.Equal(t, "no-referrer", h.Get("Referrer-Policy"))
		assert.Equal(t, "no-store", h.Get("Cache-Control"))
		assert.Contains(t, h.Get("Content-Security-Policy"), "frame-ancestors 'none'")
		if production {
			assert.Contains(t, h.Get("Strict-Transport-Security"), "max-age=")
		} else {
			assert.Empty(t, h.Get("Strict-Transport-Security"))
		}
	}
}

func TestSecurityHeaders_PresentOnPanicResponse(t *testing.T) {
	r, _ := newEngine(t, false)
	r.GET("/panic", func(c *gin.Context) { panic("x") })

	h := do(r, http.MethodGet, "/panic", nil).Header()

	assert.Equal(t, "nosniff", h.Get("X-Content-Type-Options"))
}

func TestCORS_AllowsListedOriginOnly(t *testing.T) {
	r, _ := newEngine(t, false, "https://app.example")
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	allowed := do(r, http.MethodGet, "/x", map[string]string{"Origin": "https://app.example"})
	denied := do(r, http.MethodGet, "/x", map[string]string{"Origin": "https://evil.example"})

	assert.Equal(t, "https://app.example", allowed.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, strings.ToLower(allowed.Header().Get("Access-Control-Expose-Headers")), strings.ToLower(httpx.HeaderTraceID))
	assert.Empty(t, denied.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_Preflight(t *testing.T) {
	r, _ := newEngine(t, false, "https://app.example")
	r.POST("/x", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	rec := do(r, http.MethodOptions, "/x", map[string]string{
		"Origin":                         "https://app.example",
		"Access-Control-Request-Method":  "POST",
		"Access-Control-Request-Headers": "authorization,content-type",
	})

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "https://app.example", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, strings.ToLower(rec.Header().Get("Access-Control-Allow-Headers")), "authorization")
}

func TestCORS_DisabledWithoutOrigins(t *testing.T) {
	r, _ := newEngine(t, false)
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	rec := do(r, http.MethodGet, "/x", map[string]string{"Origin": "https://app.example"})

	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestError_RateLimitedSetsRetryAfterHeader(t *testing.T) {
	r, _ := newEngine(t, false)
	r.GET("/slow", func(c *gin.Context) { httpx.Error(c, apperr.RateLimitedAfter(2*time.Minute)) })

	rec := do(r, http.MethodGet, "/slow", nil)

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, "120", rec.Header().Get("Retry-After"))
	assert.Contains(t, rec.Body.String(), `"code":"RATE_LIMITED"`)
	assert.Contains(t, rec.Body.String(), `"retry_after_seconds":120`)
}

func TestError_OtherErrorsHaveNoRetryAfter(t *testing.T) {
	r, _ := newEngine(t, false)
	r.GET("/bad", func(c *gin.Context) { httpx.Error(c, apperr.Validation(nil)) })

	rec := do(r, http.MethodGet, "/bad", nil)

	assert.Empty(t, rec.Header().Get("Retry-After"))
}
