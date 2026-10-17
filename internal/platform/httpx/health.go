package httpx

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kangsolder001/saka/internal/platform/logger"
)

const readinessTimeout = 2 * time.Second

// Pinger is a dependency whose reachability /readyz reports (the database).
type Pinger interface {
	Ping(ctx context.Context) error
}

// Healthz is the liveness probe: it only proves the process is serving.
func Healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Readyz is the readiness probe: it returns 503 while the database is
// unreachable, so orchestrators stop routing traffic to this instance.
// Failure causes go to the log, never to the response.
func Readyz(log *logger.Logger, db Pinger) gin.HandlerFunc {
	log = log.Component("health")
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), readinessTimeout)
		defer cancel()

		if err := db.Ping(ctx); err != nil {
			zl := log.FromContext(c.Request.Context())
			zl.Error().Err(err).Msg("readiness check failed")
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "unavailable",
				"checks": gin.H{"database": "down"},
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"checks": gin.H{"database": "ok"},
		})
	}
}
