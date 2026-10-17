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
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/httpx"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/outbox"
	"github.com/kangsolder001/saka/internal/user"
	emailtemplates "github.com/kangsolder001/saka/templates/email"
)

// Options tweaks the wiring; the zero value is what production uses.
type Options struct {
	// PasswordParams overrides the Argon2id cost. Tests lower it for speed.
	PasswordParams *auth.Argon2Params
}

// NewRouter builds the API: global middleware, health endpoints and every
// module's routes under /api/v1.
func NewRouter(cfg config.Config, log *logger.Logger, clk clock.Clock, database *db.DB, opts Options) (*gin.Engine, error) {
	router, err := httpx.NewRouter(cfg, log, clk, database)
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

	authService := auth.NewService(auth.Dependencies{
		Tx:     database,
		Pool:   database.Pool(),
		Users:  user.NewService(clk),
		Tokens: auth.NewUserTokens(clk),
		Mail:   mailer,
		Hasher: hasher,
		Clock:  clk,
	}, auth.Config{FrontendURL: cfg.App.FrontendURL})

	v1 := router.Group("/api/v1")
	auth.NewHandler(authService).Routes(v1)
	return router, nil
}
