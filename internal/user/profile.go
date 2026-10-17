package user

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/user/userdb"
)

// Profile field limits.
const (
	MaxNameLength      = 100
	MaxAvatarURLLength = 2048
)

// ProfileUpdate is a partial change to a user's profile (PATCH semantics):
// only the fields that are set change.
type ProfileUpdate struct {
	// Name is the new display name; nil keeps the current one.
	Name *string
	// SetAvatar says AvatarURL is part of the update. AvatarURL nil with
	// SetAvatar true removes the avatar.
	SetAvatar bool
	AvatarURL *string
}

// UpdateProfile changes the user's name and/or avatar and returns the updated
// user. Invalid values return a VALIDATION_ERROR naming the field.
func (s *Service) UpdateProfile(ctx context.Context, q db.DBTX, id uuid.UUID, in ProfileUpdate) (User, error) {
	fields := map[string]string{}
	params := userdb.UpdateProfileParams{ID: id, Now: s.clk.Now(), SetAvatar: in.SetAvatar}

	if in.Name == nil && !in.SetAvatar {
		return User{}, apperr.Validation(map[string]string{"body": "must contain at least one field to change"})
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		switch {
		case name == "":
			fields["name"] = "is required"
		case utf8.RuneCountInString(name) > MaxNameLength:
			fields["name"] = fmt.Sprintf("must be at most %d characters", MaxNameLength)
		default:
			params.Name = &name
		}
	}
	if in.SetAvatar && in.AvatarURL != nil {
		avatar := strings.TrimSpace(*in.AvatarURL)
		if reason := avatarURLProblem(avatar); reason != "" {
			fields["avatar_url"] = reason
		} else {
			params.AvatarUrl = &avatar
		}
	}
	if len(fields) > 0 {
		return User{}, apperr.Validation(fields)
	}

	row, err := userdb.New(q).UpdateProfile(ctx, params)
	if err != nil {
		return User{}, notFoundOr(err, "update profile")
	}
	s.invalidateAfterCommit(ctx, q, id)
	return fromRow(row), nil
}

// avatarURLProblem returns why raw is not an acceptable avatar URL, or "".
// Only absolute http(s) URLs are accepted, so a stored value can never be a
// javascript: or data: URL when a client renders it.
func avatarURLProblem(raw string) string {
	if len(raw) > MaxAvatarURLLength {
		return fmt.Sprintf("must be at most %d characters", MaxAvatarURLLength)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "must be a valid http(s) URL"
	}
	return ""
}
