// Package dbtest starts a real PostgreSQL 18 with testcontainers-go for
// integration tests. One container is shared per test process; each call to
// NewDatabase gets its own empty database so tests stay isolated.
package dbtest

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/kangsolder001/saka/migrations"
)

const image = "postgres:18"

var (
	once      sync.Once
	adminDSN  string
	startErr  error
	container *tcpostgres.PostgresContainer
)

func start(ctx context.Context) {
	container, startErr = tcpostgres.Run(ctx, image,
		tcpostgres.WithDatabase("postgres"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(90*time.Second),
		),
	)
	if startErr != nil {
		return
	}
	adminDSN, startErr = container.ConnectionString(ctx, "sslmode=disable")
}

// NewDatabase creates an empty database in the shared container and returns
// its connection string. The database is dropped when the test ends.
func NewDatabase(t testing.TB) string {
	t.Helper()
	ctx := context.Background()

	once.Do(func() { start(ctx) })
	if startErr != nil {
		t.Fatalf("start postgres container: %v", startErr)
	}

	name := "test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	admin, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	defer admin.Close(ctx)

	if _, err := admin.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", name)); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		conn, err := pgx.Connect(context.Background(), adminDSN)
		if err != nil {
			return
		}
		defer conn.Close(context.Background())
		_, _ = conn.Exec(context.Background(), fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", name))
	})

	return replaceDatabase(adminDSN, name)
}

// replaceDatabase swaps the database name in a postgres:// URL.
func replaceDatabase(dsn, name string) string {
	schemeEnd := strings.Index(dsn, "://") + 3
	slash := strings.Index(dsn[schemeEnd:], "/")
	if slash < 0 {
		return dsn + "/" + name
	}
	rest := dsn[schemeEnd+slash+1:]
	query := ""
	if q := strings.Index(rest, "?"); q >= 0 {
		query = rest[q:]
	}
	return dsn[:schemeEnd+slash+1] + name + query
}

// Migrate applies every migration in migrations.FS to the database at dsn.
func Migrate(t testing.TB, dsn string) {
	t.Helper()

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database/sql: %v", err)
	}
	defer sqlDB.Close()

	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		t.Fatalf("create goose provider: %v", err)
	}
	if _, err := provider.Up(context.Background()); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
}
