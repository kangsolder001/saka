//go:build integration

package org_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/org"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

// asMember builds the context the tenant middleware would give a request.
func asMember(orgID, userID uuid.UUID, role org.Role) context.Context {
	ctx := reqctx.WithOrgID(context.Background(), orgID)
	ctx = reqctx.WithUserID(ctx, userID)
	return org.WithRole(ctx, role)
}

// teamOf creates an organization with an owner, an admin and a member.
func (e *env) teamOf() (orgID uuid.UUID, owner, admin, member uuid.UUID) {
	e.t.Helper()
	o, a, m := e.newUser("owner@example.com"), e.newUser("admin@example.com"), e.newUser("member@example.com")
	created, err := e.create(o.ID, "Acme", "")
	require.NoError(e.t, err)
	for _, r := range []struct {
		id   uuid.UUID
		role string
	}{{a.ID, "admin"}, {m.ID, "member"}} {
		_, err := e.admin.Exec(context.Background(),
			"INSERT INTO memberships (organization_id, user_id, role) VALUES ($1, $2, $3)", created.ID, r.id, r.role)
		require.NoError(e.t, err)
	}
	return created.ID, o.ID, a.ID, m.ID
}

func (e *env) ownership(orgID uuid.UUID) (recorded uuid.UUID, owners int) {
	e.t.Helper()
	require.NoError(e.t, e.admin.QueryRow(context.Background(), `
		SELECT o.owner_id, (SELECT count(*) FROM memberships WHERE organization_id = o.id AND role = 'owner')
		FROM organizations o WHERE o.id = $1`, orgID).Scan(&recorded, &owners))
	return recorded, owners
}

func TestTransferOwnership_IsAtomic(t *testing.T) {
	e := newEnv(t)
	orgID, owner, admin, _ := e.teamOf()
	// Make the last step of the transfer (updating organizations) blow up.
	_, err := e.admin.Exec(context.Background(), `
		CREATE FUNCTION refuse_owner_change() RETURNS trigger LANGUAGE plpgsql AS
		$$ BEGIN RAISE EXCEPTION 'boom'; END $$;
		CREATE TRIGGER refuse_owner_change BEFORE UPDATE OF owner_id ON organizations
			FOR EACH ROW EXECUTE FUNCTION refuse_owner_change();`)
	require.NoError(t, err)

	err = e.orgs.TransferOwnership(asMember(orgID, owner, org.RoleOwner), admin, "correct-horse")

	require.Error(t, err)
	recorded, owners := e.ownership(orgID)
	assert.Equal(t, owner, recorded)
	assert.Equal(t, 1, owners, "exactly one owner, as before")
	role, _ := e.role(orgID, owner)
	assert.Equal(t, "owner", role, "the demotion was rolled back with the rest")
	role, _ = e.role(orgID, admin)
	assert.Equal(t, "admin", role, "and so was the promotion")
}

func TestTransferOwnership_RacingTransfersCannotBothWin(t *testing.T) {
	e := newEnv(t)
	orgID, owner, admin, member := e.teamOf()
	targets := []uuid.UUID{admin, member}
	results := make([]error, len(targets))

	var wg sync.WaitGroup
	for i, target := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = e.orgs.TransferOwnership(asMember(orgID, owner, org.RoleOwner), target, "correct-horse")
		}()
	}
	wg.Wait()

	succeeded := 0
	for _, err := range results {
		if err == nil {
			succeeded++
		} else {
			assert.True(t, apperr.IsCode(err, apperr.CodeForbidden), "the loser finds it is no longer the owner: %v", err)
		}
	}
	assert.Equal(t, 1, succeeded)
	recorded, owners := e.ownership(orgID)
	assert.Equal(t, 1, owners)
	role, _ := e.role(orgID, recorded)
	assert.Equal(t, "owner", role, "owner_id and the owner membership agree")
}

func TestTransferOwnership_ChecksThePasswordOfTheOwnerBeforeChangingAnything(t *testing.T) {
	e := newEnv(t)
	orgID, owner, admin, _ := e.teamOf()

	err := e.orgs.TransferOwnership(asMember(orgID, owner, org.RoleOwner), admin, "nope")

	require.True(t, apperr.IsCode(err, apperr.CodeValidation))
	assert.EqualValues(t, 1, e.passwords.calls.Load())
	recorded, _ := e.ownership(orgID)
	assert.Equal(t, owner, recorded)
}

func TestTransferOwnership_OnlyOwnersAreEvenAskedForAPassword(t *testing.T) {
	e := newEnv(t)
	orgID, _, admin, member := e.teamOf()

	err := e.orgs.TransferOwnership(asMember(orgID, admin, org.RoleAdmin), member, "correct-horse")

	assert.True(t, apperr.IsCode(err, apperr.CodeForbidden))
	assert.Zero(t, e.passwords.calls.Load(), "authorization comes before the password check")
}
