package httpx

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/ratelimit"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

// Limiter is what the rate limit middleware needs; *ratelimit.Limiter
// implements it.
type Limiter interface {
	Take(ctx context.Context, key string, rule ratelimit.Rule) (ratelimit.Decision, error)
}

// RouteLimit limits one route per client IP. Path is the route template as
// registered (for example "/api/v1/webhooks/:provider"). Routes that share a
// Rule (same Rule.Name) share one counter per IP.
type RouteLimit struct {
	Method string
	Path   string
	Rule   ratelimit.Rule
	// FailOpen lets the request through when Redis cannot be reached. The
	// default is to fail closed (500), like the limits inside the auth module.
	// Only set it where something stronger than the limit already protects the
	// route, such as the signature of a payment webhook: dropping a genuine
	// notification costs more than letting an unlimited burst reach a check
	// that rejects forgeries anyway.
	FailOpen bool
}

// RateLimitByIP applies limits to the routes they name, counting one event per
// request per client IP (the direct peer unless the proxy is a configured
// trusted proxy). It is a global middleware: it looks the matched route up in
// a table, so modules do not have to know about it. Requests that match no
// entry pass untouched. A denied request gets 429 RATE_LIMITED with Retry-After
// and never reaches the handler.
func RateLimitByIP(limiter Limiter, log *logger.Logger, limits []RouteLimit) gin.HandlerFunc {
	log = log.Component("ratelimit")
	byRoute := make(map[string]RouteLimit, len(limits))
	for _, l := range limits {
		byRoute[routeKey(l.Method, l.Path)] = l
	}
	return func(c *gin.Context) {
		limit, ok := byRoute[routeKey(c.Request.Method, c.FullPath())]
		if !ok {
			c.Next()
			return
		}
		d, err := limiter.Take(c.Request.Context(), ratelimit.Key(limit.Rule.Name, c.ClientIP()), limit.Rule)
		switch {
		case err != nil && limit.FailOpen:
			zl := log.FromContext(c.Request.Context())
			zl.Error().Err(err).Str("rule", limit.Rule.Name).Msg("rate limiter unavailable, letting the request through")
		case err != nil:
			Error(c, err)
			return
		case !d.Allowed:
			Error(c, apperr.RateLimitedAfter(d.RetryAfter))
			return
		}
		c.Next()
	}
}

// RateLimitByUser counts one event of rule per authenticated user. Mount it
// after the authentication middleware; without a user on the context it
// answers 401 (a wiring bug must not silently skip the limit). Redis being
// unreachable fails closed.
func RateLimitByUser(limiter Limiter, rule ratelimit.Rule) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := reqctx.UserID(c.Request.Context())
		if !ok {
			Error(c, apperr.Unauthenticated())
			return
		}
		d, err := limiter.Take(c.Request.Context(), ratelimit.Key(rule.Name, userID.String()), rule)
		if err != nil {
			Error(c, err)
			return
		}
		if !d.Allowed {
			Error(c, apperr.RateLimitedAfter(d.RetryAfter))
			return
		}
		c.Next()
	}
}

func routeKey(method, path string) string {
	return strings.ToUpper(method) + " " + path
}
