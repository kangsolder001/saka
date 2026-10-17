package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/mail"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/user"
)

const currentPassword = "current-password"

// accountUser is an active user whose password is currentPassword.
func (f *fixture) accountUser(t *testing.T) user.User {
	t.Helper()
	hash, err := f.hasher.Hash(currentPassword)
	require.NoError(t, err)
	return user.User{ID: uuid.New(), Email: "ann@example.com", Name: "Ann", PasswordHash: hash, Status: user.StatusActive}
}

func fieldsOf(t *testing.T, err error) map[string]string {
	t.Helper()
	require.True(t, apperr.IsCode(err, apperr.CodeValidation), "got %v", err)
	return apperr.From(err).Details["fields"].(map[string]string)
}

func TestChangePassword_SetsTheHashRevokesOtherSessionsAndNotifies(t *testing.T) {
	f := newFixture(t)
	u := f.accountUser(t)
	session := uuid.New()
	f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()
	f.expectTx()
	f.users.EXPECT().SetPasswordHash(mock.Anything, mock.Anything, u.ID, mock.MatchedBy(func(hash string) bool {
		ok, _, err := f.hasher.Verify("brand-new-password", hash)
		return err == nil && ok
	})).Return(nil).Once()
	f.refresh.EXPECT().RevokeOtherSessions(mock.Anything, mock.Anything, u.ID, session).Return(nil).Once()
	f.mail.EXPECT().Send(mock.Anything, mock.Anything, mail.Email{
		Template: "password_changed", To: "ann@example.com", Data: map[string]any{"name": "Ann"},
	}).Return(nil).Once()

	err := f.svc.ChangePassword(context.Background(), u.ID, session, currentPassword, "brand-new-password")

	require.NoError(t, err)
	assert.Equal(t, 1, f.limiter.resets, "a correct current password clears earlier failures")
	assert.Empty(t, f.limiter.records)
}

func TestChangePassword_WrongCurrentPasswordChangesNothingAndCountsAsAFailure(t *testing.T) {
	f := newFixture(t) // no tx/session/mail expectations: nothing may change
	u := f.accountUser(t)
	f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()

	err := f.svc.ChangePassword(context.Background(), u.ID, uuid.New(), "not-the-password", "brand-new-password")

	assert.Equal(t, "is incorrect", fieldsOf(t, err)["current_password"])
	assert.Equal(t, []string{"password_confirm"}, f.limiter.records, "the failed guess is counted")
	assert.Zero(t, f.limiter.resets)
}

func TestChangePassword_TooManyWrongGuessesAreRateLimitedWithoutCheckingThePassword(t *testing.T) {
	f := newFixture(t)
	f.limiter.denyCheck = true
	u := f.accountUser(t)
	f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()

	// Even the right password is refused while locked, so a guesser learns nothing.
	err := f.svc.ChangePassword(context.Background(), u.ID, uuid.New(), currentPassword, "brand-new-password")

	assert.True(t, apperr.IsCode(err, apperr.CodeRateLimited))
	assert.Empty(t, f.limiter.records, "attempts made while locked are not counted again")
}

func TestChangePassword_NewPasswordMustFollowThePolicy(t *testing.T) {
	tests := map[string]struct {
		newPassword string
		reason      string
	}{
		"too short":         {"short", "at least"},
		"same as the email": {"ann@example.com", "email"},
		"unchanged":         {currentPassword, "different"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t) // no tx expectation: nothing is written
			u := f.accountUser(t)
			// "too short" is rejected before the user is even read.
			if tt.newPassword != "short" {
				f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()
			}

			err := f.svc.ChangePassword(context.Background(), u.ID, uuid.New(), currentPassword, tt.newPassword)

			assert.Contains(t, fieldsOf(t, err)["new_password"], tt.reason)
			assert.NotContains(t, fieldsOf(t, err), "password", "the error names the request's own field")
		})
	}
}

func TestChangePassword_FailureAnywhereInTheChainFailsTheWholeChange(t *testing.T) {
	boom := errors.New("step failed")
	tests := map[string]func(f *fixture){
		"set password": func(f *fixture) {
			f.users.EXPECT().SetPasswordHash(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(boom).Once()
		},
		"revoke sessions": func(f *fixture) {
			f.users.EXPECT().SetPasswordHash(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
			f.refresh.EXPECT().RevokeOtherSessions(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(boom).Once()
		},
		"queue notice": func(f *fixture) {
			f.users.EXPECT().SetPasswordHash(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
			f.refresh.EXPECT().RevokeOtherSessions(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
			f.mail.EXPECT().Send(mock.Anything, mock.Anything, mock.Anything).Return(boom).Once()
		},
	}
	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			u := f.accountUser(t)
			f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()
			f.expectTx()
			setup(f)

			err := f.svc.ChangePassword(context.Background(), u.ID, uuid.New(), currentPassword, "brand-new-password")

			assert.ErrorIs(t, err, boom, "the transaction rolls back, so no half-applied change")
		})
	}
}

func TestChangePassword_UnknownUserIsNotFound(t *testing.T) {
	f := newFixture(t)
	f.users.EXPECT().GetByID(mock.Anything, mock.Anything, mock.Anything).Return(user.User{}, apperr.NotFound()).Once()

	err := f.svc.ChangePassword(context.Background(), uuid.New(), uuid.New(), currentPassword, "brand-new-password")

	assert.True(t, apperr.IsCode(err, apperr.CodeNotFound))
}
