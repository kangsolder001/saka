//go:build integration

package org_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

// newTwoTenants arranges two organizations, each with an owner and one open
// invitation, through the owner connection (which ignores policies). It returns
// the environment and, per tenant, the organization and owner IDs.
func newTwoTenants(t *testing.T) (*env, map[string]string, map[string]string) {
	t.Helper()
	e := newEnv(t)
	ann, bob := e.newUser("ann@example.com"), e.newUser("bob@example.com")
	a, err := e.create(ann.ID, "Alpha", "")
	require.NoError(t, err)
	b, err := e.create(bob.ID, "Beta", "")
	require.NoError(t, err)
	for _, o := range []struct{ id, owner string }{{a.ID.String(), ann.ID.String()}, {b.ID.String(), bob.ID.String()}} {
		_, err := e.admin.Exec(t.Context(), `
			INSERT INTO invitations (organization_id, email, role, token_hash, invited_by, expires_at)
			VALUES ($1::uuid, 'guest-' || $1::text || '@example.com', 'member', 'hash-' || $1::text, $2::uuid, now() + interval '7 days')`, o.id, o.owner)
		require.NoError(t, err)
	}
	return e, map[string]string{"id": a.ID.String(), "owner": ann.ID.String()}, map[string]string{"id": b.ID.String(), "owner": bob.ID.String()}
}

func inTenantTx(t *testing.T, e *env, orgID string, fn func(ctx context.Context, tx pgx.Tx)) {
	t.Helper()
	ctx := t.Context()
	if orgID != "" {
		id := mustUUID(t, orgID)
		ctx = reqctx.WithOrgID(ctx, id)
	}
	require.NoError(t, e.db.WithTx(ctx, func(tx pgx.Tx) error {
		fn(ctx, tx)
		return nil
	}))
}

func rowCount(ctx context.Context, t *testing.T, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, table string) int {
	t.Helper()
	var n int
	require.NoError(t, q.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n))
	return n
}

var tenantTables = []string{"organizations", "memberships", "invitations"}

func TestRLS_TheApplicationRunsAsTheRestrictedRole(t *testing.T) {
	e := newEnv(t)

	var user string
	var super, bypass bool
	require.NoError(t, e.db.Pool().QueryRow(t.Context(), `
		SELECT current_user, r.rolsuper, r.rolbypassrls FROM pg_roles r WHERE r.rolname = current_user`).Scan(&user, &super, &bypass))

	assert.Equal(t, "saka_app", user)
	assert.False(t, super)
	assert.False(t, bypass)
}

func TestRLS_ForgettingToSetTheTenantReturnsNothing(t *testing.T) {
	ctx := t.Context()
	e, _, _ := newTwoTenants(t)

	for _, table := range tenantTables {
		assert.NotZero(t, rowCount(ctx, t, e.admin, table), "%s: the data exists", table)
		assert.Zero(t, rowCount(ctx, t, e.db.Pool(), table), "%s: a query on the plain pool sees nothing", table)
		inTenantTx(t, e, "", func(ctx context.Context, tx pgx.Tx) {
			assert.Zero(t, rowCount(ctx, t, tx, table), "%s: WithTx without a tenant on the context sees nothing", table)
		})
	}
}

func TestRLS_AQueryWithoutAnOrganizationFilterOnlyReturnsTheTenant(t *testing.T) {
	e, a, b := newTwoTenants(t)

	inTenantTx(t, e, a["id"], func(ctx context.Context, tx pgx.Tx) {
		for _, table := range tenantTables {
			// No WHERE clause at all: the bug row-level security exists to contain.
			assert.Equal(t, 1, rowCount(ctx, t, tx, table), "%s: only tenant A's row", table)
		}
		var otherVisible int
		require.NoError(t, tx.QueryRow(ctx,
			"SELECT count(*) FROM memberships WHERE organization_id = $1", b["id"]).Scan(&otherVisible))
		assert.Zero(t, otherVisible, "asking for tenant B by name still finds nothing")
	})
	inTenantTx(t, e, b["id"], func(ctx context.Context, tx pgx.Tx) {
		var owner string
		require.NoError(t, tx.QueryRow(ctx, "SELECT user_id::text FROM memberships").Scan(&owner))
		assert.Equal(t, b["owner"], owner)
	})
}

func TestRLS_ATenantCannotWriteToAnotherTenant(t *testing.T) {
	e, a, b := newTwoTenants(t)
	ctx := t.Context()

	err := e.db.WithTx(reqctx.WithOrgID(ctx, mustUUID(t, a["id"])), func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO invitations (organization_id, email, role, token_hash, invited_by, expires_at)
			VALUES ($1, 'x@example.com', 'member', 'h', $2, now() + interval '1 day')`, b["id"], a["owner"])
		return err
	})
	assert.ErrorContains(t, err, "row-level security")

	inTenantTx(t, e, a["id"], func(ctx context.Context, tx pgx.Tx) {
		tag, err := tx.Exec(ctx, "UPDATE organizations SET name = 'hijacked' WHERE id = $1", b["id"])
		require.NoError(t, err)
		assert.Zero(t, tag.RowsAffected(), "another tenant's row is invisible to UPDATE")
		tag, err = tx.Exec(ctx, "DELETE FROM memberships WHERE organization_id = $1", b["id"])
		require.NoError(t, err)
		assert.Zero(t, tag.RowsAffected(), "and to DELETE")
	})
	var name string
	require.NoError(t, e.admin.QueryRow(ctx, "SELECT name FROM organizations WHERE id = $1", b["id"]).Scan(&name))
	assert.Equal(t, "Beta", name)
}

func TestRLS_TheTenantDoesNotLeakToTheNextUserOfAPooledConnection(t *testing.T) {
	// One connection only, so consecutive transactions are guaranteed to reuse it.
	dsn := newEnv(t).dsn
	cfg := config.Default()
	cfg.Database.URL = dsn
	cfg.Database.MaxConns = 1
	single, err := db.Connect(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(single.Close)
	ctx := t.Context()
	var orgID = mustUUID(t, "00000000-0000-7000-8000-000000000001")

	require.NoError(t, single.WithTx(reqctx.WithOrgID(ctx, orgID), func(tx pgx.Tx) error {
		var current string
		require.NoError(t, tx.QueryRow(ctx, "SELECT current_setting('app.current_org')").Scan(&current))
		assert.Equal(t, orgID.String(), current)
		return nil
	}))

	require.NoError(t, single.WithTx(ctx, func(tx pgx.Tx) error {
		var current string
		require.NoError(t, tx.QueryRow(ctx, "SELECT coalesce(current_setting('app.current_org', true), '')").Scan(&current))
		assert.Empty(t, current, "the next transaction on the same connection starts with no tenant")
		return nil
	}))
}

func TestRLS_SystemTransactionsSeeEveryTenantButDoNotLeak(t *testing.T) {
	e, _, _ := newTwoTenants(t)
	ctx := t.Context()

	require.NoError(t, e.db.WithSystemTx(ctx, func(tx pgx.Tx) error {
		for _, table := range tenantTables {
			assert.Equal(t, 2, rowCount(ctx, t, tx, table), "%s: both tenants", table)
		}
		return nil
	}))

	for _, table := range tenantTables {
		assert.Zero(t, rowCount(ctx, t, e.db.Pool(), table), "%s: the bypass ended with the system transaction", table)
	}
}

func TestRLS_ASystemTransactionIgnoresTheTenantOnTheContext(t *testing.T) {
	ctx := t.Context()
	e, a, _ := newTwoTenants(t)

	// Even with tenant A on the context, a system transaction is explicitly cross-tenant:
	// it is the caller's job to filter, and WithSystemTx never narrows silently.
	require.NoError(t, e.db.WithSystemTx(reqctx.WithOrgID(t.Context(), mustUUID(t, a["id"])), func(tx pgx.Tx) error {
		assert.Equal(t, 2, rowCount(ctx, t, tx, "organizations"))
		return nil
	}))
}

func TestRLS_MigrationsAndWorkersHaveABypass(t *testing.T) {
	ctx := t.Context()
	e, _, _ := newTwoTenants(t)

	for _, table := range tenantTables {
		assert.Equal(t, 2, rowCount(ctx, t, e.admin, table), "%s: the owner connection (migrations, the privileged path) sees all", table)
	}
}

func TestRLS_PinTenantNeedsATransactionAndMovesTheTenant(t *testing.T) {
	e, a, b := newTwoTenants(t)
	ctx := t.Context()

	assert.Error(t, db.PinTenant(ctx, e.db.Pool(), mustUUID(t, a["id"])), "on the pool the setting would last one statement")

	require.NoError(t, e.db.WithTx(reqctx.WithOrgID(ctx, mustUUID(t, a["id"])), func(tx pgx.Tx) error {
		assert.Equal(t, 1, rowCount(ctx, t, tx, "organizations"))
		require.NoError(t, db.PinTenant(ctx, tx, mustUUID(t, b["id"])))
		var name string
		require.NoError(t, tx.QueryRow(ctx, "SELECT name FROM organizations").Scan(&name))
		assert.Equal(t, "Beta", name, "the transaction now works inside tenant B")
		return nil
	}))
}

func mustUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	require.NoError(t, err)
	return id
}

func TestRLS_WithBypassLiftsPoliciesForOneStepOfATransactionOnly(t *testing.T) {
	e, _, _ := newTwoTenants(t)
	ctx := t.Context()

	assert.Error(t, db.WithBypass(ctx, e.db.Pool(), func() error { return nil }), "outside a transaction the setting would not last")

	require.NoError(t, e.db.WithTx(ctx, func(tx pgx.Tx) error {
		require.NoError(t, db.WithBypass(ctx, tx, func() error {
			assert.Equal(t, 2, rowCount(ctx, t, tx, "organizations"), "inside the bypass both tenants are visible")
			return nil
		}))
		assert.Zero(t, rowCount(ctx, t, tx, "organizations"), "and it is restored for the rest of the transaction")
		return nil
	}))
}
