//go:build integration

package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/dbtest"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

func connect(t *testing.T) *db.DB {
	t.Helper()
	dsn := dbtest.NewDatabase(t)
	cfg := config.Default()
	cfg.Database.URL = dsn

	d, err := db.Connect(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(d.Close)

	_, err = d.Pool().Exec(context.Background(), "CREATE TABLE items (id int PRIMARY KEY)")
	require.NoError(t, err)
	return d
}

func countItems(t *testing.T, d *db.DB) int {
	t.Helper()
	var n int
	require.NoError(t, d.Pool().QueryRow(context.Background(), "SELECT count(*) FROM items").Scan(&n))
	return n
}

func TestConnect_PingsDatabase(t *testing.T) {
	d := connect(t)

	assert.NoError(t, d.Ping(context.Background()))
}

func TestConnect_FailsWhenUnreachable(t *testing.T) {
	cfg := config.Default()
	cfg.Database.URL = "postgres://x:x@127.0.0.1:1/none?sslmode=disable&connect_timeout=1"

	_, err := db.Connect(context.Background(), cfg)

	assert.Error(t, err)
}

func TestWithTx_CommitsOnSuccess(t *testing.T) {
	d := connect(t)

	err := d.WithTx(context.Background(), func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), "INSERT INTO items VALUES (1), (2)")
		return err
	})

	require.NoError(t, err)
	assert.Equal(t, 2, countItems(t, d))
}

func TestWithTx_RollsBackOnError(t *testing.T) {
	d := connect(t)
	wantErr := errors.New("business failure")

	err := d.WithTx(context.Background(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(context.Background(), "INSERT INTO items VALUES (1)"); err != nil {
			return err
		}
		return wantErr
	})

	assert.ErrorIs(t, err, wantErr)
	assert.Equal(t, 0, countItems(t, d))
}

func TestWithTx_RollsBackAndRepanics(t *testing.T) {
	d := connect(t)

	assert.PanicsWithValue(t, "boom", func() {
		_ = d.WithTx(context.Background(), func(tx pgx.Tx) error {
			_, _ = tx.Exec(context.Background(), "INSERT INTO items VALUES (1)")
			panic("boom")
		})
	})

	assert.Equal(t, 0, countItems(t, d))
	// the connection must be back in the pool and usable
	assert.NoError(t, d.Ping(context.Background()))
}

func TestWithTx_RollsBackWhenContextCancelledMidway(t *testing.T) {
	d := connect(t)
	ctx, cancel := context.WithCancel(context.Background())

	err := d.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO items VALUES (1)"); err != nil {
			return err
		}
		cancel()
		return ctx.Err()
	})

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 0, countItems(t, d))
	assert.NoError(t, d.Ping(context.Background()))
}

func TestWithTx_SetsCurrentOrgFromContext(t *testing.T) {
	d := connect(t)
	orgID := uuid.New()
	ctx := reqctx.WithOrgID(context.Background(), orgID)

	var inside string
	err := d.WithTx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT current_setting($1, true)", db.CurrentOrgSetting).Scan(&inside)
	})

	require.NoError(t, err)
	assert.Equal(t, orgID.String(), inside)

	// SET LOCAL semantics: the value must not survive the transaction.
	var after *string
	require.NoError(t, d.Pool().QueryRow(context.Background(),
		"SELECT current_setting($1, true)", db.CurrentOrgSetting).Scan(&after))
	if after != nil {
		assert.Empty(t, *after)
	}
}

func TestWithTx_NoOrgInContextLeavesSettingEmpty(t *testing.T) {
	d := connect(t)

	var inside *string
	err := d.WithTx(context.Background(), func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT current_setting($1, true)", db.CurrentOrgSetting).Scan(&inside)
	})

	require.NoError(t, err)
	if inside != nil {
		assert.Empty(t, *inside)
	}
}

func TestAfterCommit_RunsOnlyAfterTheTransactionCommitted(t *testing.T) {
	d := connect(t)
	ctx := context.Background()
	var visibleInHook int

	err := d.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO items VALUES (1)"); err != nil {
			return err
		}
		db.AfterCommit(tx, func() {
			// The hook sees the committed row through a different connection.
			_ = d.Pool().QueryRow(ctx, "SELECT count(*) FROM items").Scan(&visibleInHook)
		})
		assert.Zero(t, visibleInHook, "the hook must not run while the transaction is still open")
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 1, visibleInHook, "after commit the data is durable and visible")
}

func TestAfterCommit_NeverRunsWhenTheTransactionRollsBackOrPanics(t *testing.T) {
	d := connect(t)
	ctx := context.Background()
	var ran int

	_ = d.WithTx(ctx, func(tx pgx.Tx) error {
		db.AfterCommit(tx, func() { ran++ })
		return errors.New("fails")
	})
	assert.PanicsWithValue(t, "boom", func() {
		_ = d.WithTx(ctx, func(tx pgx.Tx) error {
			db.AfterCommit(tx, func() { ran++ })
			panic("boom")
		})
	})

	assert.Zero(t, ran)
}

func TestAfterCommit_RunsHooksInRegistrationOrder(t *testing.T) {
	d := connect(t)
	var order []int

	require.NoError(t, d.WithTx(context.Background(), func(tx pgx.Tx) error {
		for i := 1; i <= 3; i++ {
			db.AfterCommit(tx, func() { order = append(order, i) })
		}
		return nil
	}))

	assert.Equal(t, []int{1, 2, 3}, order)
}

func TestAfterCommit_RunsImmediatelyOutsideATransaction(t *testing.T) {
	d := connect(t)
	ran := false

	db.AfterCommit(d.Pool(), func() { ran = true })

	assert.True(t, ran, "on the pool each statement is already committed")
}
