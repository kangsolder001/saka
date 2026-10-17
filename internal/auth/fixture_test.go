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
	mail    *mailmocks.MockSender
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
		mail:    mailmocks.NewMockSender(t),
	}
	f.jwt = newJWT(t, f.clock)
	f.svc = auth.NewService(auth.Dependencies{
		Tx: f.tx, Pool: stubDB{}, Users: f.users, Tokens: f.tokens, Refresh: f.refresh,
		JWT: f.jwt, Mail: f.mail, Hasher: f.hasher, Clock: f.clock, Log: logger.Nop(),
	}, auth.Config{FrontendURL: "https://app.example.com/"})
	return f
}

// expectTx makes the mocked Transactor run the function like a real one:
// the function's error is returned (a real transaction would roll back).
func (f *fixture) expectTx() {
	f.tx.EXPECT().WithTx(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, fn func(pgx.Tx) error) error { return fn(nil) }).Once()
}
