// Package seed prepares a development database: it checks the plan catalogue
// that the migrations ship and creates a verified demo account with its own
// organization, through the same services the API uses. cmd/seed is a thin
// wrapper; the logic lives here so it can be tested.
package seed

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/billing"
	"github.com/kangsolder001/saka/internal/org"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/user"
)

// The demo account. The password is public on purpose: it only ever exists in
// development databases (Run refuses to run in production).
const (
	DemoEmail    = "demo@example.com"
	DemoPassword = "demo-password-123"
	DemoName     = "Demo User"
	DemoOrgName  = "Demo Organization"
	DemoOrgSlug  = "demo"
)

// requiredPlans are the plan codes the application relies on (spec §9.3); they
// are created by migration 00008, not by this command.
var requiredPlans = []string{billing.FreePlanCode, "pro", "business"}

// PasswordHasher hashes the demo password (*auth.PasswordHasher implements it).
type PasswordHasher interface {
	Hash(password string) (string, error)
}

// Dependencies are what Run needs, built by the command (or a test).
type Dependencies struct {
	Config config.Config
	DB     *db.DB
	Clock  clock.Clock
	Hasher PasswordHasher
}

// Run seeds the database and reports what it did on out. It is idempotent:
// running it again changes nothing once the demo account exists. It refuses to
// run when the configured environment is production.
func Run(ctx context.Context, deps Dependencies, out io.Writer) error {
	if deps.Config.IsProduction() {
		return errors.New("seed: refusing to run in the production environment (app.env)")
	}

	billingService := billing.NewService(deps.Clock)
	users := user.NewService(deps.Clock)
	// Creating an organization needs the user directory and the subscription
	// starter only; invitations, limits and mail are not involved.
	organizations := org.NewService(org.Dependencies{Clock: deps.Clock, Tx: deps.DB, Users: users, Subscriptions: billingService})

	if err := reportPlans(ctx, deps, billingService, out); err != nil {
		return err
	}

	var created bool
	err := deps.DB.WithTx(ctx, func(tx pgx.Tx) error {
		_, err := users.GetByEmail(ctx, tx, DemoEmail)
		switch {
		case err == nil:
			return nil // already seeded
		case !apperr.IsCode(err, apperr.CodeNotFound):
			return err
		}
		hash, err := deps.Hasher.Hash(DemoPassword)
		if err != nil {
			return fmt.Errorf("hash demo password: %w", err)
		}
		demo, err := users.Create(ctx, tx, user.NewUser{Email: DemoEmail, PasswordHash: hash, Name: DemoName})
		if err != nil {
			return err
		}
		if err := users.MarkEmailVerified(ctx, tx, demo.ID); err != nil {
			return err
		}
		if _, err := organizations.CreateWithOwner(ctx, tx, demo.ID, DemoOrgName, DemoOrgSlug); err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return fmt.Errorf("seed: demo account: %w", err)
	}

	if created {
		_, _ = fmt.Fprintf(out, "created demo account\n")
	} else {
		_, _ = fmt.Fprintf(out, "demo account already exists, left as it is\n")
	}
	_, _ = fmt.Fprintf(out, "  email:    %s\n  password: %s\n  org:      %s (%s), on the free plan\n", DemoEmail, DemoPassword, DemoOrgName, DemoOrgSlug)
	return nil
}

// reportPlans lists the plan catalogue and fails when a plan the application
// needs is missing, which means the migrations were not applied.
func reportPlans(ctx context.Context, deps Dependencies, billingService *billing.Service, out io.Writer) error {
	plans, err := billingService.ActivePlans(ctx, deps.DB.Pool())
	if err != nil {
		return fmt.Errorf("seed: read plans: %w", err)
	}
	have := map[string]bool{}
	names := make([]string, 0, len(plans))
	for _, p := range plans {
		have[p.Code] = true
		names = append(names, p.Code)
	}
	var missing []string
	for _, code := range requiredPlans {
		if !have[code] {
			missing = append(missing, code)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("seed: plans missing from the database: %s (run `make migrate`)", strings.Join(missing, ", "))
	}
	_, _ = fmt.Fprintf(out, "plans: %s\n", strings.Join(names, ", "))
	return nil
}
