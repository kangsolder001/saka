//go:build integration

package org_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/apperr"
)

var epoch = time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)

func TestCreateWithOwner_CreatesOrganizationAndOwnerMembershipTogether(t *testing.T) {
	e := newEnv(t)
	ann := e.newUser("ann@example.com")

	got, err := e.create(ann.ID, "  Acme Corp ", "")

	require.NoError(t, err)
	assert.Equal(t, "Acme Corp", got.Name)
	assert.Equal(t, "acme-corp", got.Slug)
	assert.Equal(t, ann.ID, got.OwnerID)
	assert.True(t, epoch.Equal(got.CreatedAt), "created_at comes from the injected clock")
	role, member := e.role(got.ID, ann.ID)
	assert.True(t, member)
	assert.Equal(t, "owner", role)
}

func TestCreateWithOwner_ATakenDerivedSlugGetsASuffix(t *testing.T) {
	e := newEnv(t)
	ann, bob := e.newUser("ann@example.com"), e.newUser("bob@example.com")
	first, err := e.create(ann.ID, "Acme", "")
	require.NoError(t, err)

	second, err := e.create(bob.ID, "Acme", "")

	require.NoError(t, err)
	assert.Equal(t, "acme", first.Slug)
	assert.Regexp(t, regexp.MustCompile(`^acme-[0-9a-f]{6}$`), second.Slug)
}

func TestCreateWithOwner_AnExplicitSlugThatIsTakenIsAConflict(t *testing.T) {
	e := newEnv(t)
	ann, bob := e.newUser("ann@example.com"), e.newUser("bob@example.com")
	_, err := e.create(ann.ID, "Acme", "acme")
	require.NoError(t, err)

	_, err = e.create(bob.ID, "Other", "ACME")

	assert.True(t, apperr.IsCode(err, apperr.CodeConflict), "slugs are case-insensitive")
	assert.Equal(t, 1, e.count("SELECT count(*) FROM organizations"))
	assert.Zero(t, e.count("SELECT count(*) FROM memberships WHERE user_id = $1", bob.ID))
}

func TestCreateWithOwner_InvalidInputCreatesNothing(t *testing.T) {
	tests := map[string]struct {
		name, slug, field string
	}{
		"blank name":    {"   ", "", "name"},
		"name too long": {strings.Repeat("x", 101), "", "name"},
		"bad slug":      {"Acme", "not a slug", "slug"},
		"short slug":    {"Acme", "ab", "slug"},
	}
	for label, tt := range tests {
		t.Run(label, func(t *testing.T) {
			e := newEnv(t)
			ann := e.newUser("ann@example.com")

			_, err := e.create(ann.ID, tt.name, tt.slug)

			require.True(t, apperr.IsCode(err, apperr.CodeValidation))
			assert.Contains(t, apperr.From(err).Details["fields"], tt.field)
			assert.Zero(t, e.count("SELECT count(*) FROM organizations"))
		})
	}
}

func TestCreateWithOwner_RollsBackWithTheCallersTransaction(t *testing.T) {
	e := newEnv(t)
	ann := e.newUser("ann@example.com")
	boom := errors.New("a later step failed")

	err := e.db.WithTx(context.Background(), func(tx pgx.Tx) error {
		if _, err := e.orgs.CreateWithOwner(context.Background(), tx, ann.ID, "Acme", ""); err != nil {
			return err
		}
		return boom
	})

	require.ErrorIs(t, err, boom)
	assert.Zero(t, e.count("SELECT count(*) FROM organizations"))
	assert.Zero(t, e.count("SELECT count(*) FROM memberships"))
}

func TestCreateWithOwner_ADeletedOrganizationKeepsItsSlugReserved(t *testing.T) {
	e := newEnv(t)
	ann, bob := e.newUser("ann@example.com"), e.newUser("bob@example.com")
	first, err := e.create(ann.ID, "Acme", "")
	require.NoError(t, err)
	_, err = e.db.Pool().Exec(context.Background(), "UPDATE organizations SET deleted_at = now() WHERE id = $1", first.ID)
	require.NoError(t, err)

	second, err := e.create(bob.ID, "Acme", "")

	require.NoError(t, err)
	assert.NotEqual(t, "acme", second.Slug, "the old URL cannot be taken over")
}
