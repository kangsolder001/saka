package httpx

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kangsolder001/saka/internal/platform/logger"
)

const readinessTimeout = 2 * time.Second

// Pinger is a dependency whose reachability /readyz reports (database, Redis).
type Pinger interface {
	Ping(ctx context.Context) error
}

// Healthz is the liveness probe: it only proves the process is serving.
func Healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Readyz is the readiness probe: it returns 503 while any dependency is
// unreachable, so orchestrators stop routing traffic to this instance. The
// response names each check but never the failure cause, which goes to the log.
func Readyz(log *logger.Logger, checks map[string]Pinger) gin.HandlerFunc {
	log = log.Component("health")
	names := make([]string, 0, len(checks))
	for name := range checks {
		names = append(names, name)
	}
	sort.Strings(names)

	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), readinessTimeout)
		defer cancel()

		results := gin.H{}
		healthy := true
		for _, name := range names {
			if err := checks[name].Ping(ctx); err != nil {
				zl := log.FromContext(c.Request.Context())
				zl.Error().Err(err).Str("check", name).Msg("readiness check failed")
				results[name] = "down"
				healthy = false
				continue
			}
			results[name] = "ok"
		}

		if !healthy {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable", "checks": results})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "checks": results})
	}
}
