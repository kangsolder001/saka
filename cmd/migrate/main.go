// Command migrate applies the embedded goose migrations.
//
//	migrate up       apply all pending migrations
//	migrate down     roll back the last migration
//	migrate status   show applied and pending migrations
//
// It uses the same configuration as the API and reads the migrations embedded
// in the binary, so deployments need neither the goose CLI nor the SQL files.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/pressly/goose/v3"

	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/migrations"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: migrate <up|down|status>")
	}
	command := args[0]

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sqlDB, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}

	switch command {
	case "up":
		results, err := provider.Up(ctx)
		if err != nil {
			return fmt.Errorf("apply migrations: %w", err)
		}
		if len(results) == 0 {
			fmt.Println("no pending migrations")
		}
		for _, r := range results {
			fmt.Printf("applied  %s (%s)\n", r.Source.Path, r.Duration)
		}
	case "down":
		result, err := provider.Down(ctx)
		if err != nil {
			return fmt.Errorf("roll back migration: %w", err)
		}
		fmt.Printf("rolled back  %s (%s)\n", result.Source.Path, result.Duration)
	case "status":
		statuses, err := provider.Status(ctx)
		if err != nil {
			return fmt.Errorf("read migration status: %w", err)
		}
		for _, s := range statuses {
			state := "pending"
			if s.State == goose.StateApplied {
				state = "applied " + s.AppliedAt.UTC().Format("2006-01-02 15:04:05Z")
			}
			fmt.Printf("%-40s %s\n", s.Source.Path, state)
		}
	default:
		return fmt.Errorf("unknown command %q (want up, down or status)", command)
	}
	return nil
}
