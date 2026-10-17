// Package db owns the PostgreSQL connection pool and the transaction helper
// every multi-table operation goes through.
package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

// CurrentOrgSetting is the PostgreSQL setting row-level security policies
// read to find the tenant of the running transaction.
const CurrentOrgSetting = "app.current_org"

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
func (d *DB) WithTx(ctx context.Context, fn func(tx pgx.Tx) error) (err error) {
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

	if orgID, ok := reqctx.OrgID(ctx); ok {
		if _, err := tx.Exec(ctx, "SELECT set_config($1, $2, true)", CurrentOrgSetting, orgID.String()); err != nil {
			return fmt.Errorf("set current organization: %w", err)
		}
	}

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	committed = true
	return nil
}
