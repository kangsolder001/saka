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
	defer func() { _ = sqlDB.Close() }()
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

func TestOutboxJobs_Defaults(t *testing.T) {
	conn, _ := newConn(t)

	var (
		status   string
		attempts int
		version  int
		due      bool
		locked   *string
		trace    *string
	)
	err := conn.QueryRow(context.Background(), `
		INSERT INTO outbox_jobs (type) VALUES ('email.send')
		RETURNING status, attempts, uuid_extract_version(id), next_attempt_at <= now(), locked_at::text, trace_id`).
		Scan(&status, &attempts, &version, &due, &locked, &trace)

	require.NoError(t, err)
	assert.Equal(t, "pending", status)
	assert.Equal(t, 0, attempts)
	assert.Equal(t, 7, version)
	assert.True(t, due, "new jobs are due immediately")
	assert.Nil(t, locked)
	assert.Nil(t, trace)
}

func TestOutboxJobs_RejectsUnknownStatus(t *testing.T) {
	conn, _ := newConn(t)

	_, err := conn.Exec(context.Background(), "INSERT INTO outbox_jobs (type, status) VALUES ('x', 'stuck')")

	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "23514", pgErr.Code, "check_violation")
}

func createUser(t *testing.T, conn *pgx.Conn, email string) string {
	t.Helper()
	var id string
	require.NoError(t, conn.QueryRow(context.Background(),
		"INSERT INTO users (email, password_hash, name) VALUES ($1, 'h', 'n') RETURNING id", email).Scan(&id))
	return id
}

func TestUserTokens_Constraints(t *testing.T) {
	conn, _ := newConn(t)
	userID := createUser(t, conn, "t@example.com")
	insert := func(tokenType, hash string) error {
		_, err := conn.Exec(context.Background(),
			"INSERT INTO user_tokens (user_id, type, token_hash, expires_at) VALUES ($1, $2, $3, now() + interval '1 hour')",
			userID, tokenType, hash)
		return err
	}

	require.NoError(t, insert("email_verification", "hash-1"))

	var pgErr *pgconn.PgError
	require.ErrorAs(t, insert("email_verification", "hash-1"), &pgErr)
	assert.Equal(t, "23505", pgErr.Code, "token_hash is unique")
	require.ErrorAs(t, insert("magic_link", "hash-2"), &pgErr)
	assert.Equal(t, "23514", pgErr.Code, "type is restricted")
}

func TestUserTokens_AreRemovedWithTheirUser(t *testing.T) {
	conn, _ := newConn(t)
	userID := createUser(t, conn, "cascade@example.com")
	_, err := conn.Exec(context.Background(),
		"INSERT INTO user_tokens (user_id, type, token_hash, expires_at) VALUES ($1, 'password_reset', 'h', now())", userID)
	require.NoError(t, err)
	_, err = conn.Exec(context.Background(),
		"INSERT INTO refresh_tokens (user_id, family_id, token_hash, expires_at) VALUES ($1, uuidv7(), 'r', now())", userID)
	require.NoError(t, err)

	_, err = conn.Exec(context.Background(), "DELETE FROM users WHERE id = $1", userID)
	require.NoError(t, err)

	var tokens, refresh int
	require.NoError(t, conn.QueryRow(context.Background(), "SELECT (SELECT count(*) FROM user_tokens), (SELECT count(*) FROM refresh_tokens)").Scan(&tokens, &refresh))
	assert.Zero(t, tokens)
	assert.Zero(t, refresh)
}

func TestRefreshTokens_Defaults(t *testing.T) {
	conn, _ := newConn(t)
	userID := createUser(t, conn, "r@example.com")

	var (
		version int
		revoked *string
		replace *string
	)
	err := conn.QueryRow(context.Background(), `
		INSERT INTO refresh_tokens (user_id, family_id, token_hash, expires_at)
		VALUES ($1, uuidv7(), 'rt-1', now() + interval '30 days')
		RETURNING uuid_extract_version(id), revoked_at::text, replaced_by::text`, userID).Scan(&version, &revoked, &replace)

	require.NoError(t, err)
	assert.Equal(t, 7, version)
	assert.Nil(t, revoked)
	assert.Nil(t, replace)
}

func TestRefreshTokens_TokenHashIsUniqueAndReplacedByMustExist(t *testing.T) {
	conn, _ := newConn(t)
	userID := createUser(t, conn, "r2@example.com")
	_, err := conn.Exec(context.Background(),
		"INSERT INTO refresh_tokens (user_id, family_id, token_hash, expires_at) VALUES ($1, uuidv7(), 'same', now())", userID)
	require.NoError(t, err)

	var pgErr *pgconn.PgError
	_, err = conn.Exec(context.Background(),
		"INSERT INTO refresh_tokens (user_id, family_id, token_hash, expires_at) VALUES ($1, uuidv7(), 'same', now())", userID)
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "23505", pgErr.Code)

	_, err = conn.Exec(context.Background(),
		"INSERT INTO refresh_tokens (user_id, family_id, token_hash, expires_at, replaced_by) VALUES ($1, uuidv7(), 'other', now(), uuidv7())", userID)
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "23503", pgErr.Code, "replaced_by must reference an existing token")
}

func TestRateLimitHits_IsAnAppendOnlyCounterTable(t *testing.T) {
	conn, _ := newConn(t)

	for range 2 {
		_, err := conn.Exec(context.Background(), "INSERT INTO rate_limit_hits (key, created_at) VALUES ('login:abc', now())")
		require.NoError(t, err)
	}

	var n int
	require.NoError(t, conn.QueryRow(context.Background(), "SELECT count(*) FROM rate_limit_hits WHERE key = 'login:abc'").Scan(&n))
	assert.Equal(t, 2, n, "several events per key are the point of the table")
}
