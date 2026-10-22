//go:build integration

package org_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/apperr"
)

func TestMembership_ReturnsTheCallersRole(t *testing.T) {
	e := newEnv(t)
	ann := e.newUser("ann@example.com")
	acme, err := e.create(ann.ID, "Acme", "")
	require.NoError(t, err)

	m, err := e.orgs.Membership(context.Background(), acme.ID, ann.ID)

	require.NoError(t, err)
	assert.Equal(t, "owner", string(m.Role))
	assert.Equal(t, acme.ID, m.OrganizationID)
	assert.Equal(t, ann.ID, m.UserID)
}

func TestMembership_NotFoundForNonMembersUnknownAndDeletedOrganizations(t *testing.T) {
	e := newEnv(t)
	ann, bob := e.newUser("ann@example.com"), e.newUser("bob@example.com")
	acme, err := e.create(ann.ID, "Acme", "")
	require.NoError(t, err)
	gone, err := e.create(ann.ID, "Gone", "")
	require.NoError(t, err)
	_, err = e.admin.Exec(context.Background(), "UPDATE organizations SET deleted_at = now() WHERE id = $1", gone.ID)
	require.NoError(t, err)

	tests := map[string]struct{ org, user uuid.UUID }{
		"someone else's organization": {acme.ID, bob.ID},
		"an unknown organization":     {uuid.New(), ann.ID},
		"a deleted organization":      {gone.ID, ann.ID},
	}
	for label, tt := range tests {
		t.Run(label, func(t *testing.T) {
			_, err := e.orgs.Membership(context.Background(), tt.org, tt.user)

			assert.True(t, apperr.IsCode(err, apperr.CodeNotFound))
		})
	}
}
