package httpx_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/httpx"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/ratelimit"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

// stubLimiter allows `allowed` events per key, then denies; err makes every
// call fail like an unreachable Redis.
type stubLimiter struct {
	allowed int
	err     error
	seen    map[string]int
}

func (s *stubLimiter) Take(_ context.Context, key string, _ ratelimit.Rule) (ratelimit.Decision, error) {
	if s.err != nil {
		return ratelimit.Decision{}, s.err
	}
	if s.seen == nil {
		s.seen = map[string]int{}
	}
	s.seen[key]++
	if s.seen[key] > s.allowed {
		return ratelimit.Decision{RetryAfter: 42 * time.Second}, nil
	}
	return ratelimit.Decision{Allowed: true}, nil
}

var testRule = ratelimit.Rule{Name: "hook", Limit: 2, Window: time.Minute}

func limitedEngine(l httpx.Limiter, failOpen bool) (*gin.Engine, *int) {
	hits := 0
	r := gin.New()
	r.Use(httpx.RateLimitByIP(l, logger.Nop(), []httpx.RouteLimit{
		{Method: http.MethodPost, Path: "/hook/:name", Rule: testRule, FailOpen: failOpen},
	}))
	handler := func(c *gin.Context) { hits++; c.Status(http.StatusNoContent) }
	r.POST("/hook/:name", handler)
	r.GET("/hook/:name", handler)
	r.POST("/other", handler)
	return r, &hits
}

func call(r http.Handler, method, target, remote string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, target, nil)
	req.RemoteAddr = remote + ":1234"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestRateLimitByIP_DeniesAfterTheLimitWithRetryAfterAndSkipsTheHandler(t *testing.T) {
	r, hits := limitedEngine(&stubLimiter{allowed: 2}, false)

	assert.Equal(t, http.StatusNoContent, call(r, http.MethodPost, "/hook/a", "198.51.100.1").Code)
	assert.Equal(t, http.StatusNoContent, call(r, http.MethodPost, "/hook/b", "198.51.100.1").Code, "one counter per route template, not per URL")
	denied := call(r, http.MethodPost, "/hook/a", "198.51.100.1")

	assert.Equal(t, http.StatusTooManyRequests, denied.Code)
	assert.Equal(t, "42", denied.Header().Get("Retry-After"))
	assert.Contains(t, denied.Body.String(), `"code":"RATE_LIMITED"`)
	assert.Equal(t, 2, *hits, "the denied request never reached the handler")
}

func TestRateLimitByIP_CountsEachClientAddressSeparately(t *testing.T) {
	r, _ := limitedEngine(&stubLimiter{allowed: 1}, false)

	require.Equal(t, http.StatusNoContent, call(r, http.MethodPost, "/hook/a", "198.51.100.1").Code)
	require.Equal(t, http.StatusTooManyRequests, call(r, http.MethodPost, "/hook/a", "198.51.100.1").Code)

	assert.Equal(t, http.StatusNoContent, call(r, http.MethodPost, "/hook/a", "203.0.113.9").Code)
}

func TestRateLimitByIP_OnlyTheListedMethodAndRouteAreLimited(t *testing.T) {
	r, _ := limitedEngine(&stubLimiter{allowed: 0}, false)

	assert.Equal(t, http.StatusNoContent, call(r, http.MethodGet, "/hook/a", "198.51.100.1").Code, "same path, other method")
	assert.Equal(t, http.StatusNoContent, call(r, http.MethodPost, "/other", "198.51.100.1").Code, "other route")
	assert.Equal(t, http.StatusTooManyRequests, call(r, http.MethodPost, "/hook/a", "198.51.100.1").Code)
}

func TestRateLimitByIP_FailsClosedWhenTheLimiterIsDownUnlessToldOtherwise(t *testing.T) {
	down := &stubLimiter{err: errors.New("redis down")}

	closed, closedHits := limitedEngine(down, false)
	assert.Equal(t, http.StatusInternalServerError, call(closed, http.MethodPost, "/hook/a", "198.51.100.1").Code)
	assert.Zero(t, *closedHits)

	open, openHits := limitedEngine(down, true)
	assert.Equal(t, http.StatusNoContent, call(open, http.MethodPost, "/hook/a", "198.51.100.1").Code)
	assert.Equal(t, 1, *openHits)
}

func TestRateLimitByUser_CountsPerUserAndRefusesWithoutOne(t *testing.T) {
	limiter := &stubLimiter{allowed: 1}
	asUser := func(id uuid.UUID) gin.HandlerFunc {
		return func(c *gin.Context) {
			c.Request = c.Request.WithContext(reqctx.WithUserID(c.Request.Context(), id))
		}
	}
	build := func(authn gin.HandlerFunc) *gin.Engine {
		r := gin.New()
		r.POST("/create", authn, httpx.RateLimitByUser(limiter, testRule), func(c *gin.Context) { c.Status(http.StatusNoContent) })
		return r
	}
	ann, bob := uuid.New(), uuid.New()

	assert.Equal(t, http.StatusNoContent, call(build(asUser(ann)), http.MethodPost, "/create", "198.51.100.1").Code)
	assert.Equal(t, http.StatusTooManyRequests, call(build(asUser(ann)), http.MethodPost, "/create", "203.0.113.9").Code,
		"the limit follows the user, not the address")
	assert.Equal(t, http.StatusNoContent, call(build(asUser(bob)), http.MethodPost, "/create", "198.51.100.1").Code)
	assert.Equal(t, http.StatusUnauthorized, call(build(func(*gin.Context) {}), http.MethodPost, "/create", "198.51.100.1").Code,
		"no user on the context is a wiring bug and must not skip the limit")
}
