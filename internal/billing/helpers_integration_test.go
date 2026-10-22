//go:build integration

package billing_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/billing"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/dbtest"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

var epoch = time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)

// env is a migrated database with the billing service on top of it.
type env struct {
	t     *testing.T
	db    *db.DB        // the application's connection: restricted by row-level security
	admin *pgxpool.Pool // the owner's: sees every tenant, for arranging and asserting
	clock *clock.Fake
	svc   *billing.Service
	seq   int
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dsn := dbtest.NewDatabase(t)
	dbtest.Migrate(t, dsn)
	cfg := config.Default()
	cfg.Database.URL = dsn
	d, err := db.Connect(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(d.Close)
	clk := clock.NewFake(epoch)
	return &env{t: t, db: d, admin: dbtest.AdminPool(t, dsn), clock: clk, svc: billing.NewService(clk)}
}

// newOrg inserts a user, an organization and its owner membership, without a
// subscription, through the owner connection.
func (e *env) newOrg() uuid.UUID {
	e.t.Helper()
	e.seq++
	ctx := context.Background()
	var ownerID, orgID uuid.UUID
	require.NoError(e.t, e.admin.QueryRow(ctx,
		"INSERT INTO users (email, password_hash, name) VALUES ($1, 'h', 'n') RETURNING id",
		fmt.Sprintf("owner%d@example.com", e.seq)).Scan(&ownerID))
	tx, err := e.admin.Begin(ctx)
	require.NoError(e.t, err)
	require.NoError(e.t, tx.QueryRow(ctx,
		"INSERT INTO organizations (name, slug, owner_id) VALUES ('Org', $1, $2) RETURNING id",
		fmt.Sprintf("org-%d", e.seq), ownerID).Scan(&orgID))
	_, err = tx.Exec(ctx, "INSERT INTO memberships (organization_id, user_id, role) VALUES ($1, $2, 'owner')", orgID, ownerID)
	require.NoError(e.t, err)
	require.NoError(e.t, tx.Commit(ctx))
	return orgID
}

// subscribe replaces the organization's subscription with the given state,
// through the owner connection.
func (e *env) subscribe(orgID uuid.UUID, planCode string, status billing.Status, graceUntil *time.Time) {
	e.t.Helper()
	ctx := context.Background()
	_, err := e.admin.Exec(ctx, "DELETE FROM subscriptions WHERE organization_id = $1", orgID)
	require.NoError(e.t, err)
	var trialEnd *time.Time
	if status == billing.StatusTrialing {
		end := epoch.Add(14 * 24 * time.Hour)
		trialEnd = &end
	}
	_, err = e.admin.Exec(ctx, `
		INSERT INTO subscriptions (organization_id, plan_id, status, trial_ends_at, current_period_start, current_period_end, grace_until)
		SELECT $1, p.id, $3, $4, $5, $5::timestamptz + interval '30 days', $6 FROM plans p WHERE p.code = $2`,
		orgID, planCode, string(status), trialEnd, epoch, graceUntil)
	require.NoError(e.t, err)
}

// inTenant runs fn in a transaction pinned to the organization, as a request would.
func (e *env) inTenant(orgID uuid.UUID, fn func(ctx context.Context, tx pgx.Tx)) {
	e.t.Helper()
	ctx := reqctx.WithOrgID(context.Background(), orgID)
	require.NoError(e.t, e.db.WithTx(ctx, func(tx pgx.Tx) error {
		fn(ctx, tx)
		return nil
	}))
}
