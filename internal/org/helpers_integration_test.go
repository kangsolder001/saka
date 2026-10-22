//go:build integration

package org_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/org"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/dbtest"
	"github.com/kangsolder001/saka/internal/user"
)

// env is a migrated database with the services the org tests drive directly.
type env struct {
	t     *testing.T
	db    *db.DB
	clock *clock.Fake
	users *user.Service
	orgs  *org.Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dsn := dbtest.NewDatabase(t)
	dbtest.Migrate(t, dsn)

	cfg := config.Default()
	cfg.Database.URL = dsn
	d, err := db.Connect(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(d.Close)

	clk := clock.NewFake(epoch)
	return &env{t: t, db: d, clock: clk, users: user.NewService(clk), orgs: org.NewService(clk)}
}

// newUser inserts a user and returns it.
func (e *env) newUser(email string) user.User {
	e.t.Helper()
	u, err := e.users.Create(context.Background(), e.db.Pool(), user.NewUser{Email: email, PasswordHash: "x", Name: email})
	require.NoError(e.t, err)
	return u
}

func (e *env) count(sql string, args ...any) int {
	e.t.Helper()
	var n int
	require.NoError(e.t, e.db.Pool().QueryRow(context.Background(), sql, args...).Scan(&n), sql)
	return n
}

func (e *env) role(orgID, userID uuid.UUID) (string, bool) {
	e.t.Helper()
	var role string
	err := e.db.Pool().QueryRow(context.Background(),
		"SELECT role FROM memberships WHERE organization_id = $1 AND user_id = $2", orgID, userID).Scan(&role)
	return role, err == nil
}

// create runs CreateWithOwner in its own transaction, as callers do.
func (e *env) create(ownerID uuid.UUID, name, slug string) (org.Organization, error) {
	e.t.Helper()
	var created org.Organization
	err := e.db.WithTx(context.Background(), func(tx pgx.Tx) error {
		var err error
		created, err = e.orgs.CreateWithOwner(context.Background(), tx, ownerID, name, slug)
		return err
	})
	return created, err
}
