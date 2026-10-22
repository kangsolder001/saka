//go:build integration

package seed_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/auth"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/dbtest"
	"github.com/kangsolder001/saka/internal/seed"
)

type env struct {
	deps  seed.Dependencies
	admin *pgxpool.Pool
	hash  *auth.PasswordHasher
}

func newEnv(t *testing.T, mutate ...func(*config.Config)) *env {
	t.Helper()
	dsn := dbtest.NewDatabase(t)
	dbtest.Migrate(t, dsn)
	cfg := config.Default()
	cfg.Database.URL = dsn
	for _, m := range mutate {
		m(&cfg)
	}
	d, err := db.Connect(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(d.Close)
	hasher, err := auth.NewPasswordHasher(auth.Argon2Params{MemoryKiB: 64, Iterations: 1, Parallelism: 1})
	require.NoError(t, err)
	return &env{
		deps:  seed.Dependencies{Config: cfg, DB: d, Clock: clock.NewFake(time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)), Hasher: hasher},
		admin: dbtest.AdminPool(t, dsn),
		hash:  hasher,
	}
}

func (e *env) count(t *testing.T, sql string) int {
	t.Helper()
	var n int
	require.NoError(t, e.admin.QueryRow(context.Background(), sql).Scan(&n))
	return n
}

func TestRun_CreatesAVerifiedDemoAccountWithAnOrganizationOnFree(t *testing.T) {
	e := newEnv(t)
	var out bytes.Buffer

	require.NoError(t, seed.Run(t.Context(), e.deps, &out))

	var hash string
	var verified bool
	require.NoError(t, e.admin.QueryRow(t.Context(),
		"SELECT password_hash, email_verified_at IS NOT NULL FROM users WHERE email = $1", seed.DemoEmail).Scan(&hash, &verified))
	assert.True(t, verified, "the demo user can use verified-only features at once")
	ok, _, err := e.hash.Verify(seed.DemoPassword, hash)
	require.NoError(t, err)
	assert.True(t, ok, "the password printed is the one that works")

	var orgName, slug, role, plan, status string
	require.NoError(t, e.admin.QueryRow(t.Context(), `
		SELECT o.name, o.slug, m.role, p.code, s.status
		FROM organizations o
		JOIN memberships m ON m.organization_id = o.id
		JOIN users u ON u.id = m.user_id AND u.email = $1
		JOIN subscriptions s ON s.organization_id = o.id
		JOIN plans p ON p.id = s.plan_id`, seed.DemoEmail).Scan(&orgName, &slug, &role, &plan, &status))
	assert.Equal(t, seed.DemoOrgName, orgName)
	assert.Equal(t, seed.DemoOrgSlug, slug)
	assert.Equal(t, "owner", role)
	assert.Equal(t, "free", plan)
	assert.Equal(t, "active", status)

	assert.Contains(t, out.String(), "plans: free, pro, business")
	assert.Contains(t, out.String(), seed.DemoEmail)
	assert.Contains(t, out.String(), seed.DemoPassword)
	assert.Contains(t, out.String(), "created demo account")
}

func TestRun_IsIdempotent(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, seed.Run(t.Context(), e.deps, &bytes.Buffer{}))
	var second bytes.Buffer

	require.NoError(t, seed.Run(t.Context(), e.deps, &second))

	assert.Equal(t, 1, e.count(t, "SELECT count(*) FROM users"))
	assert.Equal(t, 1, e.count(t, "SELECT count(*) FROM organizations"))
	assert.Equal(t, 1, e.count(t, "SELECT count(*) FROM subscriptions"))
	assert.Contains(t, second.String(), "already exists")
	assert.NotContains(t, second.String(), "created demo account")
}

func TestRun_LeavesAnExistingDemoAccountAlone(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, seed.Run(t.Context(), e.deps, &bytes.Buffer{}))
	_, err := e.admin.Exec(t.Context(), "UPDATE users SET name = 'Renamed' WHERE email = $1", seed.DemoEmail)
	require.NoError(t, err)

	require.NoError(t, seed.Run(t.Context(), e.deps, &bytes.Buffer{}))

	var name string
	require.NoError(t, e.admin.QueryRow(t.Context(), "SELECT name FROM users WHERE email = $1", seed.DemoEmail).Scan(&name))
	assert.Equal(t, "Renamed", name, "seeding again is not a reset")
}

func TestRun_RefusesToRunInProduction(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.App.Env = config.EnvProduction })

	err := seed.Run(t.Context(), e.deps, &bytes.Buffer{})

	require.ErrorContains(t, err, "production")
	assert.Zero(t, e.count(t, "SELECT count(*) FROM users"), "nothing was written")
}

func TestRun_FailsWhenAPlanIsMissingFromTheDatabase(t *testing.T) {
	e := newEnv(t)
	_, err := e.admin.Exec(t.Context(), "DELETE FROM plans WHERE code = 'business'")
	require.NoError(t, err)

	err = seed.Run(t.Context(), e.deps, &bytes.Buffer{})

	require.ErrorContains(t, err, "business")
	require.ErrorContains(t, err, "make migrate")
	assert.Zero(t, e.count(t, "SELECT count(*) FROM users"), "no account on a half-prepared database")
}

func TestRun_FailsWhenThereAreNoPlansAtAll(t *testing.T) {
	e := newEnv(t)
	_, err := e.admin.Exec(t.Context(), "DELETE FROM plans")
	require.NoError(t, err)

	err = seed.Run(t.Context(), e.deps, &bytes.Buffer{})

	require.ErrorContains(t, err, "free, pro, business")
}
