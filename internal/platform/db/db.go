// Package db owns the PostgreSQL connection pool and the transaction helper
// every multi-table operation goes through.
package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

// Settings that row-level security policies read (see docs/rls.md and
// migration 00007). Both are transaction-local, so they never leak to the next
// user of a pooled connection.
const (
	// CurrentOrgSetting names the tenant of the running transaction.
	CurrentOrgSetting = "app.current_org"
	// BypassRLSSetting is "on" in system transactions that deliberately work
	// across tenants (see WithSystemTx).
	BypassRLSSetting = "app.bypass_rls" //nolint:gosec // a setting name, not a credential
)

// DBTX is satisfied by both *pgxpool.Pool and pgx.Tx. sqlc-generated code
// accepts it, so a repository works inside or outside a transaction.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// DB wraps the connection pool.
type DB struct {
	pool *pgxpool.Pool
}

// Connect opens the pool described by cfg and verifies it with a ping.
func Connect(ctx context.Context, cfg config.Config) (*DB, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.Database.URL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	poolCfg.MaxConns = cfg.Database.MaxConns
	poolCfg.MinConns = cfg.Database.MinConns
	if role := cfg.Database.AppRole; role != "" {
		// Every connection runs as the non-owner role, so row-level security
		// applies to it. SET ROLE is session-level and survives pooling.
		poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			if _, err := conn.Exec(ctx, "SET ROLE "+pgx.Identifier{role}.Sanitize()); err != nil {
				return fmt.Errorf("switch to role %q (run the migrations first): %w", role, err)
			}
			return nil
		}
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &DB{pool: pool}, nil
}

// Pool exposes the underlying pool for code that needs it directly
// (for example sqlc queries running outside a transaction).
func (d *DB) Pool() *pgxpool.Pool { return d.pool }

// Ping checks that the database is reachable (used by /readyz).
func (d *DB) Ping(ctx context.Context) error { return d.pool.Ping(ctx) }

// Close releases every connection in the pool.
func (d *DB) Close() { d.pool.Close() }

// WithTx runs fn inside a transaction. It commits when fn returns nil and
// rolls back when fn returns an error or panics (the panic is re-raised).
//
// When ctx carries an organization ID (set by the tenant middleware), the
// transaction is pinned to that tenant with SET LOCAL app.current_org so
// row-level security policies apply. Operations inside one transaction must
// run sequentially: pgx.Tx is not safe for concurrent use.
func (d *DB) WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	return d.run(ctx, false, fn)
}

// WithSystemTx is WithTx for the few flows that legitimately cross tenants:
// listing a user's organizations, resolving an invitation token, background
// jobs that sweep every subscription. It sets app.bypass_rls so the policies
// let the transaction see every tenant. It never sets a tenant, even if ctx
// carries one, and callers must put an explicit filter in every query.
//
// Keep its use small and greppable: row-level security protects against
// queries that forgot their tenant filter, not against code that asks to skip
// it.
func (d *DB) WithSystemTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	return d.run(ctx, true, fn)
}

func (d *DB) run(ctx context.Context, system bool, fn func(tx pgx.Tx) error) (err error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	committed := false
	defer func() {
		if committed {
			return
		}
		// Use a fresh context: ctx may already be cancelled, and the
		// rollback must still reach the server to release the connection.
		rollbackErr := tx.Rollback(context.WithoutCancel(ctx))
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			err = errors.Join(err, fmt.Errorf("rollback transaction: %w", rollbackErr))
		}
	}()

	switch orgID, hasOrg := reqctx.OrgID(ctx); {
	case system:
		if _, err := tx.Exec(ctx, "SELECT set_config($1, 'on', true)", BypassRLSSetting); err != nil {
			return fmt.Errorf("enable system access: %w", err)
		}
	case hasOrg:
		if err := pinTenant(ctx, tx, orgID); err != nil {
			return err
		}
	}

	hooked := &hookTx{Tx: tx}
	if err := fn(hooked); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	committed = true
	hooked.runAfterCommit()
	return nil
}

func pinTenant(ctx context.Context, q DBTX, orgID uuid.UUID) error {
	if _, err := q.Exec(ctx, "SELECT set_config($1, $2, true)", CurrentOrgSetting, orgID.String()); err != nil {
		return fmt.Errorf("set current organization: %w", err)
	}
	return nil
}

// PinTenant points the rest of the running transaction at orgID, for flows that
// create the tenant inside the transaction (a new organization has no tenant
// on the request context yet, but its own rows must pass the policy). It
// refuses to run outside a transaction, where the setting would silently
// last for one statement only.
func PinTenant(ctx context.Context, q DBTX, orgID uuid.UUID) error {
	if _, ok := q.(pgx.Tx); !ok {
		return errors.New("db: PinTenant needs a transaction")
	}
	return pinTenant(ctx, q, orgID)
}

// WithBypass lifts row-level security for fn inside the caller's transaction,
// then restores it. It is for a single cross-tenant lookup in the middle of a
// transaction that was not opened as a system one (registration needs to know
// whether the new address has been invited anywhere, before any tenant exists).
// It refuses to run outside a transaction, where the setting would not last.
func WithBypass(ctx context.Context, q DBTX, fn func() error) error {
	if _, ok := q.(pgx.Tx); !ok {
		return errors.New("db: WithBypass needs a transaction")
	}
	if _, err := q.Exec(ctx, "SELECT set_config($1, 'on', true)", BypassRLSSetting); err != nil {
		return fmt.Errorf("enable system access: %w", err)
	}
	fnErr := fn()
	if _, err := q.Exec(ctx, "SELECT set_config($1, 'off', true)", BypassRLSSetting); err != nil {
		return errors.Join(fnErr, fmt.Errorf("restore row-level security: %w", err))
	}
	return fnErr
}

// CommitHooker is implemented by the transaction WithTx hands to its function.
type CommitHooker interface {
	// AfterCommit registers fn to run once the transaction has committed. It
	// never runs if the transaction rolls back.
	AfterCommit(fn func())
}

// AfterCommit runs fn after the transaction behind q commits. When q is not a
// transaction (for example the connection pool, where each statement commits
// on its own) the write has already happened, so fn runs immediately.
//
// Use it for side effects that must only follow durable data, such as dropping
// a cache entry: doing that before the commit lets a concurrent reader
// re-cache the old row.
func AfterCommit(q DBTX, fn func()) {
	if h, ok := q.(CommitHooker); ok {
		h.AfterCommit(fn)
		return
	}
	fn()
}

// hookTx is the pgx.Tx given to WithTx callbacks, extended with commit hooks.
type hookTx struct {
	pgx.Tx
	hooks []func()
}

func (t *hookTx) AfterCommit(fn func()) { t.hooks = append(t.hooks, fn) }

func (t *hookTx) runAfterCommit() {
	for _, fn := range t.hooks {
		fn()
	}
}
