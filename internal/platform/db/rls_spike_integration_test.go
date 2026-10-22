//go:build integration

package db_test

// RLS learning spike (Phase 4). It runs against throwaway tables, never the
// real schema, and pins down the behaviours the production design relies on.
// The findings are written up in docs/rls.md; the assertions here keep them true.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/dbtest"
)

const (
	spikeOrgs       = 100
	spikeRowsPerOrg = 10_000 // 1,000,000 rows in total
)

type spike struct {
	owner *pgxpool.Pool // the table owner (a superuser in the test container)
	app   *pgxpool.Pool // same server, but every connection runs SET ROLE spike_app
	orgs  []uuid.UUID
}

// newSpike builds the dummy schema: a 1M-row table with a composite index
// starting with organization_id and a policy comparing it to app.current_org.
func newSpike(t *testing.T) *spike {
	t.Helper()
	ctx := context.Background()
	dsn := dbtest.NewDatabase(t)

	owner, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(owner.Close)

	// Roles are cluster-wide, so the name is unique per test.
	role := "spike_app_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	exec(t, owner, fmt.Sprintf("CREATE ROLE %s NOLOGIN", role))
	t.Cleanup(func() {
		// The database is dropped first by its own cleanup (registered earlier,
		// so it runs later); dropping the role here is best effort.
		_, _ = owner.Exec(context.Background(), fmt.Sprintf("DROP OWNED BY %s", role))
		_, _ = owner.Exec(context.Background(), fmt.Sprintf("DROP ROLE IF EXISTS %s", role))
	})

	exec(t, owner, `
		CREATE TABLE spike_orgs (id uuid PRIMARY KEY, n int NOT NULL);
		CREATE TABLE spike_items (
			id              uuid PRIMARY KEY DEFAULT uuidv7(),
			organization_id uuid NOT NULL,
			payload         text NOT NULL
		);
		CREATE INDEX spike_items_org_idx ON spike_items (organization_id, id);`)
	exec(t, owner, fmt.Sprintf(`
		INSERT INTO spike_orgs SELECT gen_random_uuid(), g FROM generate_series(1, %d) g;
		INSERT INTO spike_items (organization_id, payload)
			SELECT o.id, 'row ' || i FROM spike_orgs o, generate_series(1, %d) i;
		ANALYZE spike_items;`, spikeOrgs, spikeRowsPerOrg))
	exec(t, owner, fmt.Sprintf("GRANT SELECT, INSERT, UPDATE, DELETE ON spike_items, spike_orgs TO %s", role))

	var orgs []uuid.UUID
	rows, err := owner.Query(ctx, "SELECT id FROM spike_orgs ORDER BY n")
	require.NoError(t, err)
	orgs, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	require.NoError(t, err)

	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, "SET ROLE "+role)
		return err
	}
	app, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(app.Close)

	return &spike{owner: owner, app: app, orgs: orgs}
}

func exec(t *testing.T, p *pgxpool.Pool, sql string) {
	t.Helper()
	_, err := p.Exec(context.Background(), sql)
	require.NoError(t, err, sql)
}

// enableRLS turns RLS on with the given policy expression for the tenant test.
func (s *spike) enableRLS(t *testing.T, tenantExpr string) {
	t.Helper()
	exec(t, s.owner, "ALTER TABLE spike_items ENABLE ROW LEVEL SECURITY")
	exec(t, s.owner, "DROP POLICY IF EXISTS tenant ON spike_items")
	exec(t, s.owner, fmt.Sprintf("CREATE POLICY tenant ON spike_items USING (organization_id = %s)", tenantExpr))
}

// The production expression: NULLIF guards the empty string a connection
// reports after a transaction-local setting ended, and the scalar sub-select
// makes PostgreSQL evaluate current_setting once per query (an InitPlan)
// instead of once per row.
const wrapped = "(SELECT NULLIF(current_setting('app.current_org', true), '')::uuid)"

// The naive expression people write first.
const naive = "NULLIF(current_setting('app.current_org', true), '')::uuid"

func inTenant(t *testing.T, p *pgxpool.Pool, org uuid.UUID, fn func(tx pgx.Tx)) {
	t.Helper()
	ctx := context.Background()
	tx, err := p.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, "SELECT set_config('app.current_org', $1, true)", org.String())
	require.NoError(t, err)
	fn(tx)
}

func count(t *testing.T, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, sql string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, q.QueryRow(context.Background(), sql, args...).Scan(&n), sql)
	return n
}

// TestRLSSpike shares one 1M-row fixture between its subtests (building it
// takes several seconds); each subtest installs the policy it needs.
func TestRLSSpike(t *testing.T) {
	s := newSpike(t)
	t.Run("owner bypasses policies but the app role does not", func(t *testing.T) { ownerBypassesAppRoleDoesNot(t, s) })
	t.Run("forgetting the where clause still returns only the tenant", func(t *testing.T) { forgottenWhereIsContained(t, s) })
	t.Run("the setting does not leak to the next user of a pooled connection", func(t *testing.T) { settingDoesNotLeak(t, s) })
	t.Run("without NULLIF an ended setting breaks queries", func(t *testing.T) { withoutNullifBreaks(t, s) })
	t.Run("writes to another tenant are rejected", func(t *testing.T) { crossTenantWritesRejected(t, s) })
	t.Run("query plans and overhead", func(t *testing.T) { plansAndOverhead(t, s) })
}

func ownerBypassesAppRoleDoesNot(t *testing.T, s *spike) {
	s.enableRLS(t, wrapped)

	// The table owner (here also a superuser) is not subject to RLS: this is why
	// the application must run as a different role (SET ROLE in AfterConnect).
	assert.Equal(t, spikeOrgs*spikeRowsPerOrg, count(t, s.owner, "SELECT count(*) FROM spike_items"))
	// The app role with no tenant set sees nothing and gets no error.
	assert.Equal(t, 0, count(t, s.app, "SELECT count(*) FROM spike_items"))
}

func forgottenWhereIsContained(t *testing.T, s *spike) {
	s.enableRLS(t, wrapped)

	inTenant(t, s.app, s.orgs[7], func(tx pgx.Tx) {
		// A query with NO organization filter: the "bug" RLS exists to contain.
		assert.Equal(t, spikeRowsPerOrg, count(t, tx, "SELECT count(*) FROM spike_items"))
		assert.Equal(t, 0, count(t, tx, "SELECT count(*) FROM spike_items WHERE organization_id = $1", s.orgs[8]),
			"asking for another tenant explicitly finds nothing")
	})
}

func settingDoesNotLeak(t *testing.T, s *spike) {
	s.enableRLS(t, wrapped)
	ctx := context.Background()

	// One connection, used by two consecutive "requests".
	conn, err := s.app.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, "SELECT set_config('app.current_org', $1, true)", s.orgs[0].String())
	require.NoError(t, err)
	require.Equal(t, spikeRowsPerOrg, count(t, tx, "SELECT count(*) FROM spike_items"))
	require.NoError(t, tx.Commit(ctx))

	// After commit the setting reads as '' (not NULL) on this connection. A
	// policy casting it straight to uuid would raise "invalid input syntax";
	// the NULLIF in the policy turns it into "no tenant", so zero rows.
	var leaked string
	require.NoError(t, conn.QueryRow(ctx, "SELECT current_setting('app.current_org', true)").Scan(&leaked))
	assert.Equal(t, "", leaked)
	assert.Equal(t, 0, count(t, conn, "SELECT count(*) FROM spike_items"))
}

func withoutNullifBreaks(t *testing.T, s *spike) {
	s.enableRLS(t, "(SELECT current_setting('app.current_org', true)::uuid)")
	ctx := context.Background()

	conn, err := s.app.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, "SELECT set_config('app.current_org', $1, true)", s.orgs[0].String())
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	var n int
	err = conn.QueryRow(ctx, "SELECT count(*) FROM spike_items").Scan(&n)
	assert.ErrorContains(t, err, "invalid input syntax for type uuid", "documented trap: always NULLIF")
}

func crossTenantWritesRejected(t *testing.T, s *spike) {
	s.enableRLS(t, wrapped)
	ctx := context.Background()

	inTenant(t, s.app, s.orgs[1], func(tx pgx.Tx) {
		_, err := tx.Exec(ctx, "INSERT INTO spike_items (organization_id, payload) VALUES ($1, 'mine')", s.orgs[1])
		assert.NoError(t, err)

		// Another tenant's rows are invisible to UPDATE and DELETE: zero rows affected, no error.
		tag, err := tx.Exec(ctx, "UPDATE spike_items SET payload = 'x' WHERE organization_id = $1", s.orgs[2])
		assert.NoError(t, err)
		assert.EqualValues(t, 0, tag.RowsAffected())

		// Without an explicit WITH CHECK, USING doubles as the check on INSERT/UPDATE.
		// A rejected statement aborts the transaction, so this one goes last.
		_, err = tx.Exec(ctx, "INSERT INTO spike_items (organization_id, payload) VALUES ($1, 'theirs')", s.orgs[2])
		assert.ErrorContains(t, err, "row-level security")
	})
}

func plansAndOverhead(t *testing.T, s *spike) {
	ctx := context.Background()
	org := s.orgs[42]

	// A realistic hot query: one page of one tenant's rows.
	const page = "SELECT id, payload FROM spike_items WHERE organization_id = $1 ORDER BY id LIMIT 50"
	// The dangerous one: no organization filter at all.
	const unfiltered = "SELECT count(*) FROM spike_items"

	run := func(label string, q interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	}, sql string, args ...any) (plan string, ms float64) {
		var lines []string
		// The first run warms caches and hint bits; the second is the one we report.
		for range 2 {
			rows, err := q.Query(ctx, "EXPLAIN (ANALYZE, COSTS OFF, TIMING ON) "+sql, args...)
			require.NoError(t, err)
			lines, err = pgx.CollectRows(rows, pgx.RowTo[string])
			require.NoError(t, err)
		}
		plan = strings.Join(lines, "\n")
		for _, l := range lines {
			if strings.HasPrefix(l, "Execution Time:") {
				_, _ = fmt.Sscanf(l, "Execution Time: %f ms", &ms)
			}
		}
		t.Logf("%s\n%s\n", label, plan)
		return plan, ms
	}

	// Baseline: no RLS at all, queried as the owner.
	_, base := run("no RLS, page query", s.owner, page, org)
	_, baseCount := run("no RLS, count for one org", s.owner, "SELECT count(*) FROM spike_items WHERE organization_id = $1", org)

	// RLS with the naive expression.
	s.enableRLS(t, naive)
	var naivePage, naiveCount float64
	inTenant(t, s.app, org, func(tx pgx.Tx) {
		_, naivePage = run("RLS naive expression, page query", tx, page, org)
		_, naiveCount = run("RLS naive expression, unfiltered count", tx, unfiltered)
	})

	// RLS with the wrapped expression (what production uses).
	s.enableRLS(t, wrapped)
	var wrappedPage, wrappedCount float64
	var wrappedPlan string
	inTenant(t, s.app, org, func(tx pgx.Tx) {
		_, wrappedPage = run("RLS wrapped expression, page query", tx, page, org)
		wrappedPlan, wrappedCount = run("RLS wrapped expression, unfiltered count", tx, unfiltered)
	})

	t.Logf("SUMMARY (ms)  page: none=%.2f naive=%.2f wrapped=%.2f | count: none(1 org)=%.2f naive=%.2f wrapped=%.2f",
		base, naivePage, wrappedPage, baseCount, naiveCount, wrappedCount)

	// The property worth guarding: even a query that forgot its tenant filter is
	// answered through the (organization_id, id) index, not by scanning 1M rows.
	assert.Contains(t, wrappedPlan, "spike_items_org_idx", "the policy predicate must be index-friendly")
	assert.NotContains(t, wrappedPlan, "Seq Scan")
	// Loose bound (CI machines vary a lot): the wrapped policy must not turn a
	// tenant query into a table scan. A full scan of 1M rows takes far longer.
	assert.Less(t, wrappedCount, 500.0)
	assert.Less(t, time.Duration(wrappedPage*float64(time.Millisecond)), time.Second)
}
