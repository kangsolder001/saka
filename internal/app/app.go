// Package app is the composition root of the HTTP API: it builds every module
// once and injects dependencies through constructors. cmd/api and the
// end-to-end tests both build the API through NewRouter, so tests exercise
// the same wiring that runs in production.
package app

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/kangsolder001/saka/internal/auth"
	"github.com/kangsolder001/saka/internal/mail"
	"github.com/kangsolder001/saka/internal/org"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/httpx"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/outbox"
	"github.com/kangsolder001/saka/internal/platform/ratelimit"
	"github.com/kangsolder001/saka/internal/platform/redisx"
	"github.com/kangsolder001/saka/internal/user"
	emailtemplates "github.com/kangsolder001/saka/templates/email"
)

// Options tweaks the wiring; the zero value is what production uses.
type Options struct {
	// PasswordParams overrides the Argon2id cost. Tests lower it for speed.
	PasswordParams *auth.Argon2Params
	// Organizations takes part in account deletion. Nil means auth.NoOrganizations
	// until the organization module exists (Phase 4); tests inject a guard to
	// exercise the refusal path.
	Organizations auth.OrganizationGuard
}

// NewRouter builds the API: global middleware, health endpoints and every
// module's routes under /api/v1.
func NewRouter(cfg config.Config, log *logger.Logger, clk clock.Clock, database *db.DB, redis *redisx.Client, opts Options) (*gin.Engine, error) {
	router, err := httpx.NewRouter(cfg, log, clk, map[string]httpx.Pinger{"database": database, "redis": redis})
	if err != nil {
		return nil, err
	}

	// Email: services enqueue into the outbox inside their own transaction;
	// the worker process delivers (spec §10.1).
	renderer, err := mail.NewRenderer(emailtemplates.FS, mail.AppInfo{Name: cfg.App.Name})
	if err != nil {
		return nil, err
	}
	mailer := mail.NewService(renderer, outbox.NewStore(database.Pool(), clk))

	params := auth.DefaultArgon2Params()
	if opts.PasswordParams != nil {
		params = *opts.PasswordParams
	}
	hasher, err := auth.NewPasswordHasher(params)
	if err != nil {
		return nil, fmt.Errorf("password hasher: %w", err)
	}

	jwt, err := auth.NewJWT(cfg.Auth.JWTSecret, cfg.Auth.AccessTokenTTL.Std(), clk)
	if err != nil {
		return nil, fmt.Errorf("access token signer: %w", err)
	}

	orgs := opts.Organizations
	if orgs == nil {
		orgs = auth.NoOrganizations{} // TODO(phase-4): wire the organization module
	}
	organizations := org.NewService(clk, database)
	users := user.NewService(clk, user.WithCache(redis.Namespace("user"), cfg.Redis.UserCacheTTL.Std(), log))
	authService := auth.NewService(auth.Dependencies{
		Tx:          database,
		Pool:        database.Pool(),
		Users:       users,
		Orgs:        orgs,
		Provisioner: organizations,
		Tokens:      auth.NewUserTokens(clk),
		Sessions:    auth.NewRefreshTokens(clk, cfg.Auth.RefreshTokenTTL.Std()),
		JWT:         jwt,
		Mail:        mailer,
		Hasher:      hasher,
		Limiter:     ratelimit.New(redis, clk),
		Clock:       clk,
		Log:         log,
	}, auth.Config{FrontendURL: cfg.App.FrontendURL})

	authMiddleware := auth.NewMiddleware(authService)

	v1 := router.Group("/api/v1")
	auth.NewHandler(authService, authMiddleware, auth.HandlerConfig{SecureCookie: cfg.IsProduction()}).Routes(v1)
	user.NewHandler(user.Dependencies{Profiles: users, Security: authService, Pool: database.Pool(), Organizations: organizations}).
		Routes(v1, authMiddleware.Authenticate())
	org.NewHandler(organizations).Routes(v1, authMiddleware.Authenticate())
	return router, nil
}
