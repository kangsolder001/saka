//go:build integration

package org_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/token"
)

// deleteUser runs the guard the way account deletion does: inside a transaction
// that the caller can still roll back.
func (e *env) deleteUser(userID uuid.UUID) error {
	e.t.Helper()
	return e.db.WithTx(context.Background(), func(tx pgx.Tx) error {
		return e.orgs.BeforeUserDeleted(context.Background(), tx, userID)
	})
}

func (e *env) orgDeleted(orgID uuid.UUID) bool {
	e.t.Helper()
	var deleted bool
	require.NoError(e.t, e.admin.QueryRow(context.Background(), "SELECT deleted_at IS NOT NULL FROM organizations WHERE id = $1", orgID).Scan(&deleted))
	return deleted
}

func TestGuard_AnOwnerOfASharedOrganizationMustTransferFirst(t *testing.T) {
	e := newEnv(t)
	orgID, owner, _, _ := e.teamOf()

	err := e.deleteUser(owner)

	require.Error(t, err)
	assert.True(t, apperr.IsCode(err, apperr.CodeConflict))
	assert.Equal(t, "owns_shared_organization", apperr.From(err).Details["reason"])
	assert.False(t, e.orgDeleted(orgID))
	_, member := e.role(orgID, owner)
	assert.True(t, member)
}

func TestGuard_ASoleOwnerTakesTheirOrganizationWithThemAndItsOpenInvitations(t *testing.T) {
	e := newEnv(t)
	ann := e.newUser("ann@example.com")
	acme, err := e.create(ann.ID, "Acme", "")
	require.NoError(t, err)
	_, err = e.admin.Exec(context.Background(), `INSERT INTO invitations (organization_id, email, role, token_hash, invited_by, expires_at)
		VALUES ($1, 'guest@example.com', 'member', 'h', $2, now() + interval '7 days')`, acme.ID, ann.ID)
	require.NoError(t, err)

	require.NoError(t, e.deleteUser(ann.ID))

	assert.True(t, e.orgDeleted(acme.ID))
	assert.Zero(t, e.count("SELECT count(*) FROM invitations WHERE organization_id = $1 AND revoked_at IS NULL", acme.ID))
}

func TestGuard_AMemberJustLeavesAndTheOrganizationCarriesOn(t *testing.T) {
	e := newEnv(t)
	orgID, owner, admin, member := e.teamOf()

	require.NoError(t, e.deleteUser(admin))
	require.NoError(t, e.deleteUser(member))

	assert.False(t, e.orgDeleted(orgID))
	_, adminStays := e.role(orgID, admin)
	_, memberStays := e.role(orgID, member)
	_, ownerStays := e.role(orgID, owner)
	assert.False(t, adminStays)
	assert.False(t, memberStays)
	assert.True(t, ownerStays)
}

func TestGuard_RefusalIsAllOrNothing(t *testing.T) {
	e := newEnv(t)
	shared, owner, _, _ := e.teamOf()
	solo, err := e.create(owner, "Solo", "")
	require.NoError(t, err)
	other, _ := e.create(e.newUser("zed@example.com").ID, "Zed", "")
	_, err = e.admin.Exec(context.Background(), "INSERT INTO memberships (organization_id, user_id, role) VALUES ($1, $2, 'member')", other.ID, owner)
	require.NoError(t, err)

	err = e.deleteUser(owner)

	require.Error(t, err)
	assert.False(t, e.orgDeleted(solo.ID), "the sole-owner organization survives because the whole deletion was refused")
	assert.False(t, e.orgDeleted(shared))
	_, stillInOther := e.role(other.ID, owner)
	assert.True(t, stillInOther, "and so does the membership elsewhere")
}

func TestGuard_AfterTransferringOwnershipTheFormerOwnerCanLeave(t *testing.T) {
	e := newEnv(t)
	orgID, owner, admin, _ := e.teamOf()
	require.NoError(t, e.orgs.TransferOwnership(asMember(orgID, owner, "owner"), admin, "correct-horse"))

	require.NoError(t, e.deleteUser(owner))

	assert.False(t, e.orgDeleted(orgID))
	role, _ := e.role(orgID, admin)
	assert.Equal(t, "owner", role)
	_, stays := e.role(orgID, owner)
	assert.False(t, stays)
}

func TestGuard_UsersWithoutOrganizationsAreFine(t *testing.T) {
	e := newEnv(t)

	assert.NoError(t, e.deleteUser(e.newUser("loner@example.com").ID))
}

// The guard locks an organization exclusively before counting its members;
// joining share-locks the same row. This holds the exclusive lock the way the
// guard does and checks that an invitation accept waits for it instead of
// slipping a member in between the guard's count and its cleanup.
func TestGuard_JoiningWaitsForTheOrganizationLock(t *testing.T) {
	e := newEnv(t)
	ann, bob := e.newUser("ann@example.com"), e.newUser("bob@example.com")
	acme, err := e.create(ann.ID, "Acme", "")
	require.NoError(t, err)
	raw, hash, err := token.Generate()
	require.NoError(t, err)
	_, err = e.admin.Exec(context.Background(), `INSERT INTO invitations (organization_id, email, role, token_hash, invited_by, expires_at)
		VALUES ($1, 'bob@example.com', 'member', $2, $3, now() + interval '7 days')`, acme.ID, hash, ann.ID)
	require.NoError(t, err)

	locked, release, accepted := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var releaseOnce sync.Once
	unlock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unlock() // never leave the lock holder hanging if an assertion fails first
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // the "guard": holds the organization row exclusively until released
		defer wg.Done()
		_ = e.db.WithSystemTx(context.Background(), func(tx pgx.Tx) error { // system: with no tenant set, row-level security would hide the row and nothing would be locked
			if _, err := tx.Exec(context.Background(), "SELECT id FROM organizations WHERE id = $1 FOR UPDATE", acme.ID); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked
	go func() {
		_, err := e.orgs.AcceptInvitation(context.Background(), bob.ID, raw)
		accepted <- err
	}()

	select {
	case err := <-accepted:
		t.Fatalf("joining finished while the organization was locked (err=%v): it must wait", err)
	case <-time.After(400 * time.Millisecond):
	}
	unlock()
	wg.Wait()

	select {
	case err := <-accepted:
		assert.NoError(t, err, "once the lock is gone the join proceeds")
	case <-time.After(10 * time.Second):
		t.Fatal("joining never resumed")
	}
	_, member := e.role(acme.ID, bob.ID)
	assert.True(t, member)
}
