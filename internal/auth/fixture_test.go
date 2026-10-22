package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/mock"

	"github.com/kangsolder001/saka/internal/auth"
	authmocks "github.com/kangsolder001/saka/internal/auth/mocks"
	mailmocks "github.com/kangsolder001/saka/internal/mail/mocks"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/ratelimit"
)

// stubDB satisfies db.DBTX without a database: unit tests only pass it around
// to mocked collaborators.
type stubDB struct{}

func (stubDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (stubDB) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (stubDB) QueryRow(context.Context, string, ...any) pgx.Row        { return nil }

var unitEpoch = time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)

// fixture wires an auth.Service to mocks and a fake clock.
type fixture struct {
	svc     *auth.Service
	clock   *clock.Fake
	hasher  *auth.PasswordHasher
	tx      *authmocks.MockTransactor
	users   *authmocks.MockUserService
	tokens  *authmocks.MockTokenService
	refresh *authmocks.MockRefreshStore
	jwt     *auth.JWT
	limiter *fakeLimiter
	mail    *mailmocks.MockSender
	orgs    *authmocks.MockOrganizationGuard
	prov    *authmocks.MockOrganizationProvisioner
}

// fakeLimiter allows everything by default and records what it was asked.
// Tests flip the deny switches to simulate a client over its limit.
type fakeLimiter struct {
	denyTake   bool
	denyCheck  bool
	retryAfter time.Duration

	takes   []string // rule names
	checks  []string
	records []string
	resets  int
}

func (l *fakeLimiter) decision(deny bool) ratelimit.Decision {
	if deny {
		return ratelimit.Decision{Allowed: false, RetryAfter: l.retryAfter}
	}
	return ratelimit.Decision{Allowed: true, Remaining: 1}
}

func (l *fakeLimiter) Take(_ context.Context, _ string, r ratelimit.Rule) (ratelimit.Decision, error) {
	l.takes = append(l.takes, r.Name)
	return l.decision(l.denyTake), nil
}

func (l *fakeLimiter) Check(_ context.Context, _ string, r ratelimit.Rule) (ratelimit.Decision, error) {
	l.checks = append(l.checks, r.Name)
	return l.decision(l.denyCheck), nil
}

func (l *fakeLimiter) Record(_ context.Context, _ string, r ratelimit.Rule) error {
	l.records = append(l.records, r.Name)
	return nil
}

func (l *fakeLimiter) Reset(context.Context, string) error {
	l.resets++
	return nil
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{
		clock:   clock.NewFake(unitEpoch),
		hasher:  newHasher(t),
		tx:      authmocks.NewMockTransactor(t),
		users:   authmocks.NewMockUserService(t),
		tokens:  authmocks.NewMockTokenService(t),
		refresh: authmocks.NewMockRefreshStore(t),
		limiter: &fakeLimiter{retryAfter: 90 * time.Second},
		mail:    mailmocks.NewMockSender(t),
		orgs:    authmocks.NewMockOrganizationGuard(t),
		prov:    authmocks.NewMockOrganizationProvisioner(t),
	}
	f.jwt = newJWT(t, f.clock)
	f.svc = auth.NewService(auth.Dependencies{
		Tx: f.tx, Pool: stubDB{}, Users: f.users, Orgs: f.orgs, Provisioner: f.prov, Tokens: f.tokens, Sessions: f.refresh,
		JWT: f.jwt, Mail: f.mail, Hasher: f.hasher, Limiter: f.limiter, Clock: f.clock, Log: logger.Nop(),
	}, auth.Config{FrontendURL: "https://app.example.com/"})
	return f
}

// expectTx makes the mocked Transactor run the function like a real one:
// the function's error is returned (a real transaction would roll back).
func (f *fixture) expectTx() {
	f.tx.EXPECT().WithTx(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, fn func(pgx.Tx) error) error { return fn(nil) }).Once()
}
