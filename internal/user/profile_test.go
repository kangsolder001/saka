package user_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/user"
)

// stubDB satisfies db.DBTX for tests that must fail before touching the database.
type stubDB struct{}

func (stubDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (stubDB) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (stubDB) QueryRow(context.Context, string, ...any) pgx.Row        { return nil }

func fieldErrors(t *testing.T, err error) map[string]string {
	t.Helper()
	require.True(t, apperr.IsCode(err, apperr.CodeValidation), "got %v", err)
	return apperr.From(err).Details["fields"].(map[string]string)
}

func TestUpdateProfile_RejectsInvalidInputBeforeTouchingTheDatabase(t *testing.T) {
	svc := user.NewService(clock.NewFake(time.Now()))
	longName := strings.Repeat("a", user.MaxNameLength+1)
	longURL := "https://example.com/" + strings.Repeat("a", user.MaxAvatarURLLength)
	tests := []struct {
		name  string
		in    user.ProfileUpdate
		field string
	}{
		{"nothing to change", user.ProfileUpdate{}, "body"},
		{"blank name", user.ProfileUpdate{Name: ptr("   ")}, "name"},
		{"empty name", user.ProfileUpdate{Name: ptr("")}, "name"},
		{"name too long", user.ProfileUpdate{Name: &longName}, "name"},
		{"javascript avatar", user.ProfileUpdate{SetAvatar: true, AvatarURL: ptr("javascript:alert(1)")}, "avatar_url"},
		{"data avatar", user.ProfileUpdate{SetAvatar: true, AvatarURL: ptr("data:image/png;base64,AAAA")}, "avatar_url"},
		{"relative avatar", user.ProfileUpdate{SetAvatar: true, AvatarURL: ptr("/img/a.png")}, "avatar_url"},
		{"avatar without host", user.ProfileUpdate{SetAvatar: true, AvatarURL: ptr("https://")}, "avatar_url"},
		{"avatar not a url", user.ProfileUpdate{SetAvatar: true, AvatarURL: ptr("not a url")}, "avatar_url"},
		{"avatar too long", user.ProfileUpdate{SetAvatar: true, AvatarURL: &longURL}, "avatar_url"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.UpdateProfile(context.Background(), stubDB{}, uuid.New(), tt.in)

			assert.Contains(t, fieldErrors(t, err), tt.field)
		})
	}
}

func TestUpdateProfile_ReportsEveryInvalidFieldAtOnce(t *testing.T) {
	svc := user.NewService(clock.NewFake(time.Now()))

	_, err := svc.UpdateProfile(context.Background(), stubDB{}, uuid.New(),
		user.ProfileUpdate{Name: ptr(" "), SetAvatar: true, AvatarURL: ptr("javascript:alert(1)")})

	assert.Len(t, fieldErrors(t, err), 2)
}
