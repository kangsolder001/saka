//go:build integration

package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/app"
	"github.com/kangsolder001/saka/internal/auth"
	"github.com/kangsolder001/saka/internal/billing/payment"
	"github.com/kangsolder001/saka/internal/billing/payment/fake"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/dbtest"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/redistest"
	"github.com/kangsolder001/saka/internal/platform/redisx"
	"github.com/kangsolder001/saka/internal/user"
)

func init() { gin.SetMode(gin.TestMode) }

// remoteAddrHeader is a pseudo header the test client turns into the request's RemoteAddr.
const remoteAddrHeader = "X-Test-Remote-Addr"

var epoch = time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)

// stack is the real API (router, services, PostgreSQL) driven over HTTP with
// a fake clock, for end-to-end tests.
type stack struct {
	t      *testing.T
	router http.Handler
	// engine is the same router, for tests that inspect the registered routes.
	engine *gin.Engine
	db     *db.DB // the API's own connection (restricted by row-level security)
	// admin sees every tenant: tests arrange and assert through it.
	admin *pgxpool.Pool
	redis *redisx.Client
	clock *clock.Fake
	// users shares the API's Redis cache, so tests can invalidate it the way
	// an admin or profile flow would after changing a user.
	users *user.Service
	// gateway is the fake payment gateway the API creates payments with; tests
	// drive payments by posting webhooks signed for it.
	gateway *fake.Provider
	// logs holds every line the API logged; log is the logger that writes to it
	// (tests build workers and jobs with it to capture their output too).
	logs *logBuffer
	log  *logger.Logger
}

// logBuffer is a concurrency-safe buffer for captured log output.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// entries parses the captured JSON lines.
func (b *logBuffer) entries(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(b.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &m), "every log line is one JSON object: %q", line)
		out = append(out, m)
	}
	return out
}

// newStack builds the API; tweak adjusts the wiring (see app.Options) when a test needs to.
func newStack(t *testing.T, tweak ...func(*app.Options)) *stack {
	t.Helper()
	dsn := dbtest.NewDatabase(t)
	dbtest.Migrate(t, dsn)

	cfg := config.Default()
	cfg.Database.URL = dsn
	cfg.App.FrontendURL = "https://app.example.com"
	cfg.Auth.JWTSecret = "0123456789abcdef0123456789abcdef-e2e"
	d, err := db.Connect(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(d.Close)

	clk := clock.NewFake(epoch)
	light := auth.Argon2Params{MemoryKiB: 64, Iterations: 1, Parallelism: 1}
	cfg.Redis = redistest.Config(t)
	rdb, err := redisx.Connect(context.Background(), cfg.Redis)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rdb.Close() })

	gateway := fake.New()
	registry, err := payment.NewRegistry(gateway)
	require.NoError(t, err)
	opts := app.Options{PasswordParams: &light, PaymentProviders: registry}
	for _, f := range tweak {
		f(&opts)
	}
	// Everything the API logs is captured (debug level) so tests can inspect it.
	logs := &logBuffer{}
	log, err := logger.New(logs, "debug")
	require.NoError(t, err)
	router, err := app.NewRouter(cfg, log, clk, d, rdb, opts)
	require.NoError(t, err)
	users := user.NewService(clk, user.WithCache(rdb.Namespace("user"), cfg.Redis.UserCacheTTL.Std(), logger.Nop()))
	return &stack{t: t, router: router, engine: router, db: d, admin: dbtest.AdminPool(t, dsn), redis: rdb, clock: clk, users: users, gateway: gateway, logs: logs, log: log}
}

type response struct {
	*httptest.ResponseRecorder
	t *testing.T
}

func (r response) json() map[string]any {
	r.t.Helper()
	var m map[string]any
	require.NoError(r.t, json.Unmarshal(r.Body.Bytes(), &m), r.Body.String())
	return m
}

func (s *stack) do(method, path, body string, headers map[string]string) response {
	s.t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		if k == remoteAddrHeader { // test-only: pretend to be another client address
			req.RemoteAddr = v + ":4000"
			continue
		}
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return response{ResponseRecorder: rec, t: s.t}
}

func (s *stack) post(path, body string, headers ...map[string]string) response {
	s.t.Helper()
	h := map[string]string{}
	for _, m := range headers {
		for k, v := range m {
			h[k] = v
		}
	}
	return s.do(http.MethodPost, path, body, h)
}

func (s *stack) queryRow(sql string, args ...any) interface{ Scan(...any) error } {
	return s.admin.QueryRow(context.Background(), sql, args...)
}

func authHash(raw string) string { return auth.HashToken(raw) }

// invalidateAllUsers drops every cached user summary, as the code that changes
// a user does after committing.
func (s *stack) invalidateAllUsers() {
	s.t.Helper()
	rows, err := s.admin.Query(context.Background(), "SELECT id FROM users")
	require.NoError(s.t, err)
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		require.NoError(s.t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(s.t, rows.Err())
	require.NoError(s.t, s.users.InvalidateCache(context.Background(), ids...))
}

func (s *stack) ctx() context.Context { return context.Background() }
