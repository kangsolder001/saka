//go:build integration

package user_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/user"
)

func TestUpdateProfile_ChangesOnlyTheFieldsGiven(t *testing.T) {
	e := newEnv(t)
	u := e.create(t, "ann@example.com")
	ctx := context.Background()
	e.clock.Advance(time.Hour)

	renamed, err := e.svc.UpdateProfile(ctx, e.db.Pool(), u.ID, user.ProfileUpdate{Name: ptr("  Ann Lee ")})
	require.NoError(t, err)
	assert.Equal(t, "Ann Lee", renamed.Name, "trimmed")
	assert.Nil(t, renamed.AvatarURL, "the avatar was not part of the update")
	assert.True(t, renamed.UpdatedAt.After(u.UpdatedAt))

	withAvatar, err := e.svc.UpdateProfile(ctx, e.db.Pool(), u.ID, user.ProfileUpdate{SetAvatar: true, AvatarURL: ptr("https://cdn.example.com/a.png")})
	require.NoError(t, err)
	assert.Equal(t, "Ann Lee", withAvatar.Name, "the name was not part of the update")
	require.NotNil(t, withAvatar.AvatarURL)
	assert.Equal(t, "https://cdn.example.com/a.png", *withAvatar.AvatarURL)

	removed, err := e.svc.UpdateProfile(ctx, e.db.Pool(), u.ID, user.ProfileUpdate{SetAvatar: true})
	require.NoError(t, err)
	assert.Nil(t, removed.AvatarURL, "an explicit null removes the avatar")

	stored, err := e.svc.GetByID(ctx, e.db.Pool(), u.ID)
	require.NoError(t, err)
	assert.Equal(t, "Ann Lee", stored.Name)
	assert.Nil(t, stored.AvatarURL)
}

func TestUpdateProfile_UnknownOrDeletedUserIsNotFound(t *testing.T) {
	e := newEnv(t)
	deleted := e.create(t, "gone@example.com")
	_, err := e.db.Pool().Exec(context.Background(), "UPDATE users SET deleted_at = now(), status = 'deleted' WHERE id = $1", deleted.ID)
	require.NoError(t, err)

	for _, id := range []uuid.UUID{uuid.New(), deleted.ID} {
		_, err := e.svc.UpdateProfile(context.Background(), e.db.Pool(), id, user.ProfileUpdate{Name: ptr("X")})

		assert.True(t, apperr.IsCode(err, apperr.CodeNotFound), "got %v", err)
	}
}

func TestUpdateProfile_InvalidatesTheCacheItself(t *testing.T) {
	e := newCacheEnv(t)
	u := e.create(t, "ann@example.com")
	ctx := context.Background()
	before, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
	require.NoError(t, err)
	require.Equal(t, "Ann", before.Name)

	_, err = e.svc.UpdateProfile(ctx, e.db.Pool(), u.ID, user.ProfileUpdate{Name: ptr("Ann Lee")})
	require.NoError(t, err)

	after, err := e.svc.GetSummary(ctx, e.db.Pool(), u.ID)
	require.NoError(t, err)
	assert.Equal(t, "Ann Lee", after.Name, "never stale: the write path drops the entry")
}
