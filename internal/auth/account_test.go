package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/auth"
	"github.com/kangsolder001/saka/internal/mail"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/db"
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

func TestRequestEmailChange_EmailsAConfirmationLinkToTheNewAddressOnly(t *testing.T) {
	f := newFixture(t)
	u := f.accountUser(t)
	f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()
	f.users.EXPECT().GetByEmail(mock.Anything, mock.Anything, "new@example.com").Return(user.User{}, apperr.NotFound()).Once()
	f.expectTx()
	f.tokens.EXPECT().Issue(mock.Anything, mock.Anything, u.ID, auth.TokenEmailChange, mock.MatchedBy(func(payload any) bool {
		b, err := json.Marshal(payload)
		return err == nil && string(b) == `{"new_email":"new@example.com"}`
	})).Return("raw+token", nil).Once()
	f.mail.EXPECT().Send(mock.Anything, mock.Anything, mail.Email{
		Template: "email_change_confirm", To: "new@example.com",
		Data: map[string]any{"name": "Ann", "url": "https://app.example.com/confirm-email-change?token=raw%2Btoken"},
	}).Return(nil).Once()

	err := f.svc.RequestEmailChange(context.Background(), u.ID, currentPassword, "  New@Example.COM ")

	require.NoError(t, err)
	assert.Equal(t, []string{"email_change"}, f.limiter.takes)
}

func TestRequestEmailChange_WrongPasswordSendsNothing(t *testing.T) {
	f := newFixture(t) // no lookup/tx/token/mail expectations
	u := f.accountUser(t)
	f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()

	err := f.svc.RequestEmailChange(context.Background(), u.ID, "wrong-password", "new@example.com")

	assert.Equal(t, "is incorrect", fieldsOf(t, err)["current_password"])
	assert.Empty(t, f.limiter.takes, "a failed password does not use up the email quota")
}

func TestRequestEmailChange_RejectsSameOrTakenAddresses(t *testing.T) {
	f := newFixture(t)
	u := f.accountUser(t)
	f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Twice()
	f.users.EXPECT().GetByEmail(mock.Anything, mock.Anything, "taken@example.com").
		Return(user.User{ID: uuid.New(), Email: "taken@example.com"}, nil).Once()

	same := f.svc.RequestEmailChange(context.Background(), u.ID, currentPassword, "ANN@example.com")
	taken := f.svc.RequestEmailChange(context.Background(), u.ID, currentPassword, "taken@example.com")

	assert.Contains(t, fieldsOf(t, same)["new_email"], "different")
	assert.True(t, apperr.IsCode(taken, apperr.CodeConflict))
}

func TestRequestEmailChange_IsRateLimited(t *testing.T) {
	f := newFixture(t)
	f.limiter.denyTake = true
	u := f.accountUser(t)
	f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()

	err := f.svc.RequestEmailChange(context.Background(), u.ID, currentPassword, "new@example.com")

	assert.True(t, apperr.IsCode(err, apperr.CodeRateLimited), "and before any lookup, so the limit also covers probing for registered emails")
}

func TestRequestEmailChange_LookupFailureIsNotMistakenForAvailable(t *testing.T) {
	f := newFixture(t)
	u := f.accountUser(t)
	boom := errors.New("db down")
	f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()
	f.users.EXPECT().GetByEmail(mock.Anything, mock.Anything, mock.Anything).Return(user.User{}, boom).Once()

	err := f.svc.RequestEmailChange(context.Background(), u.ID, currentPassword, "new@example.com")

	assert.ErrorIs(t, err, boom)
}

func changeToken(userID uuid.UUID, newEmail string) auth.UserToken {
	payload, _ := json.Marshal(map[string]string{"new_email": newEmail})
	return auth.UserToken{UserID: userID, Type: auth.TokenEmailChange, Payload: payload}
}

func TestConfirmEmailChange_SwitchesTheAddressAndNotifiesTheOldOne(t *testing.T) {
	f := newFixture(t)
	u := f.accountUser(t)
	f.expectTx()
	f.tokens.EXPECT().Consume(mock.Anything, mock.Anything, "raw", auth.TokenEmailChange).Return(changeToken(u.ID, "new@example.com"), nil).Once()
	f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()
	f.users.EXPECT().ChangeEmail(mock.Anything, mock.Anything, u.ID, "new@example.com").Return(nil).Once()
	f.prov.EXPECT().AcceptPendingInvitations(mock.Anything, mock.Anything, u.ID, "new@example.com").Return(nil).Once()
	f.mail.EXPECT().Send(mock.Anything, mock.Anything, mail.Email{
		Template: "email_changed_notice", To: "ann@example.com",
		Data: map[string]any{"name": "Ann", "new_email": "new@example.com"},
	}).Return(nil).Once()

	got, err := f.svc.ConfirmEmailChange(context.Background(), "raw")

	require.NoError(t, err)
	assert.Equal(t, "new@example.com", got)
}

func TestConfirmEmailChange_InvalidTokenChangesNothing(t *testing.T) {
	f := newFixture(t)
	f.expectTx()
	f.tokens.EXPECT().Consume(mock.Anything, mock.Anything, "bad", auth.TokenEmailChange).Return(auth.UserToken{}, auth.ErrInvalidToken()).Once()

	_, err := f.svc.ConfirmEmailChange(context.Background(), "bad")

	assert.True(t, apperr.IsCode(err, apperr.CodeValidation))
}

func TestConfirmEmailChange_UnusableTokensLookLikeInvalidOnes(t *testing.T) {
	u := user.User{ID: uuid.New(), Email: "ann@example.com", Name: "Ann", Status: user.StatusActive}
	tests := map[string]struct {
		token auth.UserToken
		user  func(f *fixture)
	}{
		"no payload":      {auth.UserToken{UserID: u.ID}, nil},
		"empty new email": {changeToken(u.ID, ""), nil},
		"user is gone": {changeToken(u.ID, "new@example.com"), func(f *fixture) {
			f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(user.User{}, apperr.NotFound()).Once()
		}},
		"user is suspended": {changeToken(u.ID, "new@example.com"), func(f *fixture) {
			f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(user.User{ID: u.ID, Status: user.StatusSuspended}, nil).Once()
		}},
		"already the current email": {changeToken(u.ID, "ann@example.com"), func(f *fixture) {
			f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()
		}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t) // no ChangeEmail / mail expectation
			f.expectTx()
			f.tokens.EXPECT().Consume(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(tt.token, nil).Once()
			if tt.user != nil {
				tt.user(f)
			}

			_, err := f.svc.ConfirmEmailChange(context.Background(), "raw")

			assert.True(t, apperr.IsCode(err, apperr.CodeValidation))
		})
	}
}

func TestConfirmEmailChange_FailureAnywhereFailsTheWholeChange(t *testing.T) {
	boom := errors.New("step failed")
	tests := map[string]func(f *fixture){
		"taken in the meantime": func(f *fixture) {
			f.users.EXPECT().ChangeEmail(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(apperr.Conflict("taken")).Once()
		},
		"join invited organizations": func(f *fixture) {
			f.users.EXPECT().ChangeEmail(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
			f.prov.EXPECT().AcceptPendingInvitations(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(boom).Once()
		},
		"queue notice": func(f *fixture) {
			f.users.EXPECT().ChangeEmail(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
			f.prov.EXPECT().AcceptPendingInvitations(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
			f.mail.EXPECT().Send(mock.Anything, mock.Anything, mock.Anything).Return(boom).Once()
		},
	}
	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			u := f.accountUser(t)
			f.expectTx()
			f.tokens.EXPECT().Consume(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(changeToken(u.ID, "new@example.com"), nil).Once()
			f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()
			setup(f)

			got, err := f.svc.ConfirmEmailChange(context.Background(), "raw")

			require.Error(t, err)
			assert.Empty(t, got, "no address is reported as changed when the transaction fails")
		})
	}
}

func TestListSessions_MapsTheStoreAndFlagsTheCurrentOne(t *testing.T) {
	f := newFixture(t)
	userID, current, other := uuid.New(), uuid.New(), uuid.New()
	started, used, expires := unitEpoch.Add(-48*time.Hour), unitEpoch.Add(-time.Hour), unitEpoch.Add(24*time.Hour)
	f.refresh.EXPECT().ListSessions(mock.Anything, mock.Anything, userID).Return([]auth.SessionInfo{
		{ID: other, UserAgent: "Firefox", IP: "203.0.113.9", StartedAt: started, LastUsedAt: used, ExpiresAt: expires},
		{ID: current, UserAgent: "Chrome", IP: "198.51.100.2", StartedAt: started, LastUsedAt: used, ExpiresAt: expires},
	}, nil).Once()

	got, err := f.svc.ListSessions(context.Background(), userID, current)

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, other, got[0].ID, "store order (most recently used first) is kept")
	assert.False(t, got[0].Current)
	assert.True(t, got[1].Current)
	assert.Equal(t, user.Session{
		ID: current, UserAgent: "Chrome", IP: "198.51.100.2", StartedAt: started, LastUsedAt: used, ExpiresAt: expires, Current: true,
	}, got[1])
}

func TestListSessions_NoSessionsIsAnEmptyListNotAnError(t *testing.T) {
	f := newFixture(t)
	f.refresh.EXPECT().ListSessions(mock.Anything, mock.Anything, mock.Anything).Return(nil, nil).Once()

	got, err := f.svc.ListSessions(context.Background(), uuid.New(), uuid.New())

	require.NoError(t, err)
	assert.NotNil(t, got, "serializes as [] rather than null")
	assert.Empty(t, got)
}

func TestListSessions_StoreFailureIsReturned(t *testing.T) {
	f := newFixture(t)
	boom := errors.New("db down")
	f.refresh.EXPECT().ListSessions(mock.Anything, mock.Anything, mock.Anything).Return(nil, boom).Once()

	_, err := f.svc.ListSessions(context.Background(), uuid.New(), uuid.New())

	assert.ErrorIs(t, err, boom)
}

func TestRevokeSession(t *testing.T) {
	boom := errors.New("db down")
	tests := map[string]struct {
		revoked bool
		err     error
		check   func(t *testing.T, err error)
	}{
		"ended":                    {true, nil, func(t *testing.T, err error) { assert.NoError(t, err) }},
		"not theirs or not active": {false, nil, func(t *testing.T, err error) { assert.True(t, apperr.IsCode(err, apperr.CodeNotFound)) }},
		"store failure":            {false, boom, func(t *testing.T, err error) { assert.ErrorIs(t, err, boom) }},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			userID, session := uuid.New(), uuid.New()
			f.refresh.EXPECT().RevokeUserSession(mock.Anything, mock.Anything, userID, session).Return(tt.revoked, tt.err).Once()

			tt.check(t, f.svc.RevokeSession(context.Background(), userID, session))
		})
	}
}

func TestDeleteAccount_CleansUpOrganizationsDeletesTheUserAndEndsEverySessionInOrder(t *testing.T) {
	f := newFixture(t)
	u := f.accountUser(t)
	var order []string
	f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()
	f.expectTx()
	f.orgs.EXPECT().BeforeUserDeleted(mock.Anything, mock.Anything, u.ID).RunAndReturn(
		func(context.Context, db.DBTX, uuid.UUID) error { order = append(order, "orgs"); return nil }).Once()
	f.users.EXPECT().Delete(mock.Anything, mock.Anything, u.ID).RunAndReturn(
		func(context.Context, db.DBTX, uuid.UUID) error { order = append(order, "delete"); return nil }).Once()
	f.refresh.EXPECT().RevokeAllSessions(mock.Anything, mock.Anything, u.ID).RunAndReturn(
		func(context.Context, db.DBTX, uuid.UUID) error { order = append(order, "sessions"); return nil }).Once()

	err := f.svc.DeleteAccount(context.Background(), u.ID, currentPassword)

	require.NoError(t, err)
	assert.Equal(t, []string{"orgs", "delete", "sessions"}, order, "the organization module can refuse before anything is deleted")
	assert.Equal(t, 1, f.limiter.resets)
}

func TestDeleteAccount_WrongPasswordDeletesNothing(t *testing.T) {
	f := newFixture(t) // no tx/orgs/delete/sessions expectations
	u := f.accountUser(t)
	f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()

	err := f.svc.DeleteAccount(context.Background(), u.ID, "wrong-password")

	assert.Equal(t, "is incorrect", fieldsOf(t, err)["current_password"])
	assert.Equal(t, []string{"password_confirm"}, f.limiter.records)
}

func TestDeleteAccount_RefusedWhileTheUserOwnsAnOrganizationWithOtherMembers(t *testing.T) {
	f := newFixture(t) // no Delete / RevokeAllSessions expectation: nothing may happen after the refusal
	u := f.accountUser(t)
	f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()
	f.expectTx()
	f.orgs.EXPECT().BeforeUserDeleted(mock.Anything, mock.Anything, u.ID).Return(auth.ErrOwnsSharedOrganization()).Once()

	err := f.svc.DeleteAccount(context.Background(), u.ID, currentPassword)

	require.True(t, apperr.IsCode(err, apperr.CodeConflict))
	assert.Equal(t, "owns_shared_organization", apperr.From(err).Details["reason"])
}

func TestDeleteAccount_FailureAnywhereInTheChainFailsTheWholeDeletion(t *testing.T) {
	boom := errors.New("step failed")
	tests := map[string]func(f *fixture){
		"organization cleanup": func(f *fixture) {
			f.orgs.EXPECT().BeforeUserDeleted(mock.Anything, mock.Anything, mock.Anything).Return(boom).Once()
		},
		"delete user": func(f *fixture) {
			f.orgs.EXPECT().BeforeUserDeleted(mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
			f.users.EXPECT().Delete(mock.Anything, mock.Anything, mock.Anything).Return(boom).Once()
		},
		"revoke sessions": func(f *fixture) {
			f.orgs.EXPECT().BeforeUserDeleted(mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
			f.users.EXPECT().Delete(mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
			f.refresh.EXPECT().RevokeAllSessions(mock.Anything, mock.Anything, mock.Anything).Return(boom).Once()
		},
	}
	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			u := f.accountUser(t)
			f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()
			f.expectTx()
			setup(f)

			err := f.svc.DeleteAccount(context.Background(), u.ID, currentPassword)

			assert.ErrorIs(t, err, boom, "the transaction rolls back, so the account is not half deleted")
		})
	}
}

func TestDeleteAccount_TooManyWrongGuessesAreRateLimited(t *testing.T) {
	f := newFixture(t)
	f.limiter.denyCheck = true
	u := f.accountUser(t)
	f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()

	err := f.svc.DeleteAccount(context.Background(), u.ID, currentPassword)

	assert.True(t, apperr.IsCode(err, apperr.CodeRateLimited))
}

func TestNoOrganizations_NeverBlocksDeletion(t *testing.T) {
	assert.NoError(t, auth.NoOrganizations{}.BeforeUserDeleted(context.Background(), stubDB{}, uuid.New()))
}
