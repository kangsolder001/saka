// Package httpx holds the HTTP plumbing shared by all modules: middleware,
// error responses, health endpoints and request helpers.
package httpx

import (
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

const (
	HeaderRequestID   = "X-Request-ID"
	HeaderTraceID     = "X-Trace-ID"
	HeaderTraceparent = "traceparent"
)

// RequestContext assigns a request ID and trace ID to every request, stores
// them in the request context and echoes them in response headers. An
// incoming W3C traceparent header continues the caller's trace.
func RequestContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		traceID, ok := reqctx.ParseTraceparent(c.GetHeader(HeaderTraceparent))
		if !ok {
			traceID = reqctx.NewTraceID()
		}
		requestID := reqctx.NewRequestID()

		ctx := reqctx.WithTraceID(c.Request.Context(), traceID)
		ctx = reqctx.WithRequestID(ctx, requestID)
		c.Request = c.Request.WithContext(ctx)

		c.Header(HeaderRequestID, requestID)
		c.Header(HeaderTraceID, traceID)
		c.Next()
	}
}

// AccessLog writes one structured log line per request. It logs the route
// template instead of the raw URL so tokens in paths or query strings never
// reach the logs. Errors recorded with Error are included.
func AccessLog(log *logger.Logger, clk clock.Clock) gin.HandlerFunc {
	log = log.Component("http")
	return func(c *gin.Context) {
		start := clk.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		status := c.Writer.Status()

		zl := log.FromContext(c.Request.Context())
		event := zl.Info()
		switch {
		case status >= http.StatusInternalServerError:
			event = zl.Error()
		case status >= http.StatusBadRequest:
			event = zl.Warn()
		}
		event = event.
			Str("method", c.Request.Method).
			Str("route", route).
			Int("status", status).
			Dur("duration_ms", clk.Now().Sub(start)).
			Int("bytes", c.Writer.Size()).
			Str("client_ip", c.ClientIP())
		if len(c.Errors) > 0 {
			event = event.Str("error", c.Errors.String())
		}
		event.Msg("request completed")
	}
}

// Recover turns a panic in a handler into a logged 500 response in the
// standard error format, so one bad request never takes the process down.
func Recover(log *logger.Logger) gin.HandlerFunc {
	log = log.Component("http")
	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				zl := log.FromContext(c.Request.Context())
				zl.Error().
					Interface("panic", rec).
					Bytes("stack", debug.Stack()).
					Msg("handler panicked")
				if !c.Writer.Written() {
					status, body := apperr.Internal(nil).Response()
					c.AbortWithStatusJSON(status, body)
				} else {
					c.Abort()
				}
			}
		}()
		c.Next()
	}
}

// SecurityHeaders sets conservative headers suited to a JSON API. HSTS is
// only sent in production, where HTTPS is guaranteed.
func SecurityHeaders(production bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Cache-Control", "no-store")
		if production {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		c.Next()
	}
}

// CORS allows cross-origin browser calls only from the listed origins. With
// an empty list no CORS headers are sent, so browsers keep the same-origin policy.
func CORS(allowedOrigins []string) gin.HandlerFunc {
	if len(allowedOrigins) == 0 {
		return func(c *gin.Context) { c.Next() }
	}
	return cors.New(cors.Config{
		AllowOrigins:  allowedOrigins,
		AllowMethods:  []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete, http.MethodOptions},
		AllowHeaders:  []string{"Authorization", "Content-Type", HeaderTraceparent},
		ExposeHeaders: []string{HeaderRequestID, HeaderTraceID},
		MaxAge:        12 * time.Hour,
	})
}
