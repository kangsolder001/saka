// Package app is the composition root of the HTTP API: it builds every module
// once and injects dependencies through constructors. cmd/api and the
// end-to-end tests both build the API through NewRouter, so tests exercise
// the same wiring that runs in production.
package app

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/kangsolder001/saka/internal/auth"
	"github.com/kangsolder001/saka/internal/billing"
	"github.com/kangsolder001/saka/internal/entitlement"
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
	// Organizations takes part in account deletion. Nil means the organization
	// module; tests inject a guard to exercise the refusal path.
	Organizations auth.OrganizationGuard
	// TenantRoutes, when set, mounts extra routes under /orgs/{orgId} (after
	// authentication and the tenant middleware). Tests use it to exercise
	// middleware such as RequireFeature on a route that does not exist in
	// production yet.
	TenantRoutes func(rg *gin.RouterGroup, entitlements *entitlement.Service)
}

// guardOrDefault returns the injected guard, or the organization module.
func guardOrDefault(injected auth.OrganizationGuard, organizations *org.Service) auth.OrganizationGuard {
	if injected != nil {
		return injected
	}
	return organizations
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

	users := user.NewService(clk, user.WithCache(redis.Namespace("user"), cfg.Redis.UserCacheTTL.Std(), log))
	// Account deletion consults the organization module; tests may inject a
	// guard to exercise the refusal path.
	guard := opts.Organizations

	// auth and org need each other (registration creates the organization; the
	// ownership transfer confirms a password), each through an interface the
	// consumer declares. The composition root closes the loop: org is built
	// first and receives auth once it exists.
	billingService := billing.NewService(clk)
	organizations := org.NewService(org.Dependencies{
		Clock: clk, Tx: database, Users: users, Subscriptions: billingService, Mail: mailer, FrontendURL: cfg.App.FrontendURL,
	})
	authService := auth.NewService(auth.Dependencies{
		Tx:          database,
		Pool:        database.Pool(),
		Users:       users,
		Orgs:        guardOrDefault(guard, organizations),
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

	organizations.Passwords = authService
	// entitlement counts seats through org and org enforces the limit through
	// entitlement: the loop is closed here, like the one with auth above.
	entitlements := entitlement.NewService(billingService, database)
	entitlements.RegisterCounter(org.MembersLimit, organizations.CountSeats)
	organizations.Limits = entitlements
	authMiddleware := auth.NewMiddleware(authService)

	v1 := router.Group("/api/v1")
	auth.NewHandler(authService, authMiddleware, auth.HandlerConfig{SecureCookie: cfg.IsProduction()}).Routes(v1)
	user.NewHandler(user.Dependencies{Profiles: users, Security: authService, Pool: database.Pool(), Organizations: organizations}).
		Routes(v1, authMiddleware.Authenticate())
	org.NewHandler(organizations).Routes(v1, authMiddleware.Authenticate(), authMiddleware.RequireVerifiedEmail())
	if opts.TenantRoutes != nil {
		opts.TenantRoutes(org.TenantRoutes(v1, authMiddleware.Authenticate(), organizations), entitlements)
	}
	return router, nil
}
