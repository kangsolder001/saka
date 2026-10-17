//go:build integration

package migrations_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/dbtest"
	"github.com/kangsolder001/saka/migrations"
)

func newConn(t *testing.T) (*pgx.Conn, string) {
	t.Helper()
	dsn := dbtest.NewDatabase(t)
	dbtest.Migrate(t, dsn)
	conn, err := pgx.Connect(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn, dsn
}

func insertUser(conn *pgx.Conn, email string) error {
	_, err := conn.Exec(context.Background(),
		"INSERT INTO users (email, password_hash, name) VALUES ($1, 'hash', 'Test')", email)
	return err
}

func TestUsers_Defaults(t *testing.T) {
	conn, _ := newConn(t)
	require.NoError(t, insertUser(conn, "a@example.com"))

	var (
		version   int
		status    string
		verified  *string
		createdOK bool
	)
	err := conn.QueryRow(context.Background(), `
		SELECT uuid_extract_version(id), status, email_verified_at::text, created_at <= now() AND updated_at <= now()
		FROM users`).Scan(&version, &status, &verified, &createdOK)

	require.NoError(t, err)
	assert.Equal(t, 7, version, "ids must be UUID v7")
	assert.Equal(t, "active", status)
	assert.Nil(t, verified)
	assert.True(t, createdOK)
}

func TestUsers_EmailIsCaseInsensitiveUnique(t *testing.T) {
	conn, _ := newConn(t)
	require.NoError(t, insertUser(conn, "Person@Example.com"))

	err := insertUser(conn, "person@example.COM")

	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "23505", pgErr.Code, "unique_violation")
}

func TestUsers_RejectsUnknownStatus(t *testing.T) {
	conn, _ := newConn(t)

	_, err := conn.Exec(context.Background(),
		"INSERT INTO users (email, password_hash, name, status) VALUES ('b@example.com', 'h', 'n', 'banned')")

	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "23514", pgErr.Code, "check_violation")
}

func TestUsers_RequiredColumns(t *testing.T) {
	conn, _ := newConn(t)

	_, err := conn.Exec(context.Background(), "INSERT INTO users (email) VALUES ('c@example.com')")

	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "23502", pgErr.Code, "not_null_violation")
}

func TestMigrations_AreReversible(t *testing.T) {
	_, dsn := newConn(t)
	sqlDB, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer sqlDB.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	require.NoError(t, err)
	ctx := context.Background()

	_, err = provider.DownTo(ctx, 0)
	require.NoError(t, err)

	var exists bool
	require.NoError(t, sqlDB.QueryRowContext(ctx, "SELECT to_regclass('public.users') IS NOT NULL").Scan(&exists))
	assert.False(t, exists, "down must drop users")

	_, err = provider.Up(ctx)
	assert.NoError(t, err, "up must work again after down")
}
