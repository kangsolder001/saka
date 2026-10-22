//go:build integration

package org_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/org"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

// seats counts through the application's connection, inside the tenant, as the
// entitlement counter does.
func (e *env) seats(orgID uuid.UUID) int {
	e.t.Helper()
	ctx := reqctx.WithOrgID(context.Background(), orgID)
	var n int
	require.NoError(e.t, e.db.WithTx(ctx, func(tx pgx.Tx) error {
		var err error
		n, err = e.orgs.CountSeats(ctx, tx, orgID)
		return err
	}))
	return n
}

// addInvitation inserts an invitation; closedColumn names the column that ends
// it ("revoked_at", "declined_at", "accepted_at"), empty for an open one.
func (e *env) addInvitation(orgID, inviter uuid.UUID, email string, expiresAt time.Time, closedColumn string) {
	e.t.Helper()
	column, value := "revoked_at", "NULL"
	if closedColumn != "" {
		column, value = closedColumn, "now()"
	}
	_, err := e.admin.Exec(context.Background(), `
		INSERT INTO invitations (organization_id, email, role, token_hash, invited_by, expires_at, `+column+`)
		VALUES ($1, $2, 'member', $3, $4, $5, `+value+`)`, orgID, email, uuid.NewString(), inviter, expiresAt)
	require.NoError(e.t, err)
}

func TestCountSeats_IsMembersPlusLiveInvitations(t *testing.T) {
	e := newEnv(t)
	ann := e.newUser("ann@example.com")
	acme, err := e.create(ann.ID, "Acme", "")
	require.NoError(t, err)
	now := e.clock.Now()
	require.Equal(t, 1, e.seats(acme.ID), "the owner")

	e.addInvitation(acme.ID, ann.ID, "live@example.com", now.Add(time.Hour), "")
	e.addInvitation(acme.ID, ann.ID, "lastmoment@example.com", now.Add(time.Microsecond), "")
	e.addInvitation(acme.ID, ann.ID, "expiring@example.com", now, "")
	e.addInvitation(acme.ID, ann.ID, "expired@example.com", now.Add(-time.Hour), "")
	e.addInvitation(acme.ID, ann.ID, "revoked@example.com", now.Add(time.Hour), "revoked_at")
	e.addInvitation(acme.ID, ann.ID, "declined@example.com", now.Add(time.Hour), "declined_at")
	e.addInvitation(acme.ID, ann.ID, "accepted@example.com", now.Add(time.Hour), "accepted_at")

	assert.Equal(t, 3, e.seats(acme.ID), "owner + the two invitations that can still be redeemed; the one expiring right now is already gone")

	e.clock.Advance(time.Hour)
	assert.Equal(t, 1, e.seats(acme.ID), "invitations free their seat by the clock alone, nothing has to run")
}

func TestCountSeats_OnlyCountsTheTenantItIsAskedAbout(t *testing.T) {
	e := newEnv(t)
	ann, bob := e.newUser("ann@example.com"), e.newUser("bob@example.com")
	acme, err := e.create(ann.ID, "Acme", "")
	require.NoError(t, err)
	beta, err := e.create(bob.ID, "Beta", "")
	require.NoError(t, err)
	e.addInvitation(beta.ID, bob.ID, "guest@example.com", e.clock.Now().Add(time.Hour), "")

	assert.Equal(t, 1, e.seats(acme.ID))
	assert.Equal(t, 2, e.seats(beta.ID))

	ctx := reqctx.WithOrgID(context.Background(), acme.ID)
	require.NoError(t, e.db.WithTx(ctx, func(tx pgx.Tx) error {
		n, err := e.orgs.CountSeats(ctx, tx, beta.ID)
		require.NoError(t, err)
		assert.Zero(t, n, "row-level security hides the other tenant even when asked by ID")
		return nil
	}))
}

// A service wired without a limit checker must not hand out seats for free.
func TestInvite_WithoutALimitCheckerFailsInsteadOfSkippingTheLimit(t *testing.T) {
	e := newEnv(t)
	ann := e.newUser("ann@example.com")
	acme, err := e.create(ann.ID, "Acme", "")
	require.NoError(t, err)
	e.orgs.Limits = nil
	ctx := reqctx.WithOrgID(reqctx.WithUserID(context.Background(), ann.ID), acme.ID)
	ctx = org.WithRole(ctx, org.RoleOwner)

	_, _, err = e.orgs.Invite(ctx, "guest@example.com", org.RoleMember)

	require.Error(t, err)
	assert.Zero(t, e.count("SELECT count(*) FROM invitations"))
	assert.Empty(t, e.mail.sent)
}
