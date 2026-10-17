package httpx

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/logger"
)

// NewRouter builds the gin engine with the global middleware chain and the
// health endpoints. Modules attach their routes to the returned engine
// (under /api/v1) in cmd/api.
// checks are the dependencies /readyz verifies, by name (for example
// "database" and "redis").
func NewRouter(cfg config.Config, log *logger.Logger, clk clock.Clock, checks map[string]Pinger) (*gin.Engine, error) {
	// Release mode keeps stdout JSON-only (no gin debug banner).
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	if err := r.SetTrustedProxies(cfg.App.TrustedProxies); err != nil {
		return nil, fmt.Errorf("set trusted proxies: %w", err)
	}

	r.Use(
		RequestContext(),
		AccessLog(log, clk),
		Recover(log),
		SecurityHeaders(cfg.IsProduction()),
		CORS(cfg.App.CORSAllowedOrigins),
	)
	r.NoRoute(NotFound)

	r.GET("/healthz", Healthz)
	r.GET("/readyz", Readyz(log, checks))
	return r, nil
}
