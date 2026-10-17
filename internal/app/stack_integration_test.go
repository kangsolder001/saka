//go:build integration

package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/app"
	"github.com/kangsolder001/saka/internal/auth"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/dbtest"
	"github.com/kangsolder001/saka/internal/platform/logger"
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
	db     *db.DB
	clock  *clock.Fake
}

func newStack(t *testing.T) *stack {
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
	router, err := app.NewRouter(cfg, logger.Nop(), clk, d, app.Options{PasswordParams: &light})
	require.NoError(t, err)
	return &stack{t: t, router: router, db: d, clock: clk}
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
	return s.db.Pool().QueryRow(context.Background(), sql, args...)
}

func authHash(raw string) string { return auth.HashToken(raw) }
