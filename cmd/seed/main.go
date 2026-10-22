// Command seed prepares a development database: it checks the default plans
// and creates a verified demo account with an organization. It is idempotent
// and refuses to run when app.env is production.
//
//	seed [-config path]
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/kangsolder001/saka/internal/auth"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/seed"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", config.DefaultPath, "path to the JSON config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	// Check before touching the database: a production config must not even connect.
	if cfg.IsProduction() {
		return errors.New("refusing to run in the production environment (app.env)")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := db.Connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer database.Close()

	hasher, err := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	if err != nil {
		return fmt.Errorf("password hasher: %w", err)
	}
	return seed.Run(ctx, seed.Dependencies{Config: cfg, DB: database, Clock: clock.Real{}, Hasher: hasher}, os.Stdout)
}
