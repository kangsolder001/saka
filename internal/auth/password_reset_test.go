package auth_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/auth"
	"github.com/kangsolder001/saka/internal/mail"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/user"
)

func TestForgotPassword_ActiveUserGetsAResetEmailInOneTransaction(t *testing.T) {
	f := newFixture(t)
	u := user.User{ID: uuid.New(), Email: "ann@example.com", Name: "Ann", Status: user.StatusActive}
	f.users.EXPECT().GetByEmail(mock.Anything, mock.Anything, "ann@example.com").Return(u, nil).Once()
	f.expectTx()
	f.tokens.EXPECT().Issue(mock.Anything, mock.Anything, u.ID, auth.TokenPasswordReset, nil).Return("reset+raw", nil).Once()
	f.mail.EXPECT().Send(mock.Anything, mock.Anything, mail.Email{
		Template: "password_reset", To: "ann@example.com",
		Data: map[string]any{"name": "Ann", "url": "https://app.example.com/reset-password?token=reset%2Braw"},
	}).Return(nil).Once()

	require.NoError(t, f.svc.ForgotPassword(context.Background(), "ann@example.com"))
}

func TestForgotPassword_UnknownOrInactiveAccountsSucceedSilently(t *testing.T) {
	tests := map[string]struct {
		lookup func(f *fixture)
	}{
		"unknown email": {func(f *fixture) {
			f.users.EXPECT().GetByEmail(mock.Anything, mock.Anything, mock.Anything).Return(user.User{}, apperr.NotFound()).Once()
		}},
		"suspended": {func(f *fixture) {
			f.users.EXPECT().GetByEmail(mock.Anything, mock.Anything, mock.Anything).
				Return(user.User{ID: uuid.New(), Status: user.StatusSuspended}, nil).Once()
		}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t) // no tx/token/mail expectations: nothing may be created or sent
			tt.lookup(f)

			err := f.svc.ForgotPassword(context.Background(), "ghost@example.com")

			assert.NoError(t, err, "the caller must be able to answer 200 either way")
		})
	}
}

func TestForgotPassword_InfrastructureFailureIsStillAnError(t *testing.T) {
	f := newFixture(t)
	boom := errors.New("db down")
	f.users.EXPECT().GetByEmail(mock.Anything, mock.Anything, mock.Anything).Return(user.User{}, boom).Once()

	err := f.svc.ForgotPassword(context.Background(), "ann@example.com")

	assert.ErrorIs(t, err, boom, "only 'not found' is swallowed; real failures must not look like success")
}

func (f *fixture) resetUser() user.User {
	return user.User{ID: uuid.New(), Email: "ann@example.com", Name: "Ann", Status: user.StatusActive}
}

func TestResetPassword_ConsumesTokenSetsPasswordRevokesSessionsAndNotifies(t *testing.T) {
	f := newFixture(t)
	u := f.resetUser()
	f.expectTx()
	f.tokens.EXPECT().Consume(mock.Anything, mock.Anything, "raw-reset", auth.TokenPasswordReset).
		Return(auth.UserToken{UserID: u.ID}, nil).Once()
	f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()
	f.users.EXPECT().SetPasswordHash(mock.Anything, mock.Anything, u.ID, mock.MatchedBy(func(hash string) bool {
		ok, _, err := f.hasher.Verify("brand-new-password", hash)
		return err == nil && ok
	})).Return(nil).Once()
	f.refresh.EXPECT().RevokeAllSessions(mock.Anything, mock.Anything, u.ID).Return(nil).Once()
	f.mail.EXPECT().Send(mock.Anything, mock.Anything, mail.Email{
		Template: "password_changed", To: "ann@example.com", Data: map[string]any{"name": "Ann"},
	}).Return(nil).Once()

	require.NoError(t, f.svc.ResetPassword(context.Background(), "raw-reset", "brand-new-password"))
}

func TestResetPassword_WeakPasswordFailsBeforeTouchingAnything(t *testing.T) {
	f := newFixture(t) // no expectations: the token must not even be looked at

	err := f.svc.ResetPassword(context.Background(), "raw-reset", "short")

	assert.True(t, apperr.IsCode(err, apperr.CodeValidation))
}

func TestResetPassword_InvalidTokenChangesNothing(t *testing.T) {
	f := newFixture(t)
	f.expectTx()
	f.tokens.EXPECT().Consume(mock.Anything, mock.Anything, "bad", auth.TokenPasswordReset).
		Return(auth.UserToken{}, auth.ErrInvalidToken()).Once()

	err := f.svc.ResetPassword(context.Background(), "bad", "brand-new-password")

	assert.True(t, apperr.IsCode(err, apperr.CodeValidation))
}

func TestResetPassword_PasswordEqualToEmailIsRejectedInsideTheTransaction(t *testing.T) {
	f := newFixture(t)
	u := f.resetUser()
	var txResult error
	f.tx.EXPECT().WithTx(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, fn func(pgx.Tx) error) error {
			txResult = fn(nil)
			return txResult
		}).Once()
	f.tokens.EXPECT().Consume(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(auth.UserToken{UserID: u.ID}, nil).Once()
	f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()

	err := f.svc.ResetPassword(context.Background(), "raw-reset", "ann@example.com")

	assert.True(t, apperr.IsCode(err, apperr.CodeValidation))
	assert.Error(t, txResult, "the transaction fails, so the token use is rolled back and the user can retry")
}

func TestResetPassword_InactiveOrMissingUserLooksLikeAnInvalidToken(t *testing.T) {
	tests := map[string]func(f *fixture, id uuid.UUID){
		"suspended": func(f *fixture, id uuid.UUID) {
			f.users.EXPECT().GetByID(mock.Anything, mock.Anything, id).Return(user.User{ID: id, Status: user.StatusSuspended}, nil).Once()
		},
		"gone": func(f *fixture, id uuid.UUID) {
			f.users.EXPECT().GetByID(mock.Anything, mock.Anything, id).Return(user.User{}, apperr.NotFound()).Once()
		},
	}
	for name, lookup := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			id := uuid.New()
			f.expectTx()
			f.tokens.EXPECT().Consume(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(auth.UserToken{UserID: id}, nil).Once()
			lookup(f, id)

			err := f.svc.ResetPassword(context.Background(), "raw", "brand-new-password")

			assert.True(t, apperr.IsCode(err, apperr.CodeValidation))
		})
	}
}

func TestResetPassword_FailureAnywhereInTheChainFailsTheWholeReset(t *testing.T) {
	boom := errors.New("step failed")
	tests := map[string]func(f *fixture, u user.User){
		"set password": func(f *fixture, u user.User) {
			f.users.EXPECT().SetPasswordHash(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(boom).Once()
		},
		"revoke sessions": func(f *fixture, u user.User) {
			f.users.EXPECT().SetPasswordHash(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
			f.refresh.EXPECT().RevokeAllSessions(mock.Anything, mock.Anything, mock.Anything).Return(boom).Once()
		},
		"queue notice": func(f *fixture, u user.User) {
			f.users.EXPECT().SetPasswordHash(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
			f.refresh.EXPECT().RevokeAllSessions(mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
			f.mail.EXPECT().Send(mock.Anything, mock.Anything, mock.Anything).Return(boom).Once()
		},
	}
	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			u := f.resetUser()
			f.expectTx()
			f.tokens.EXPECT().Consume(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(auth.UserToken{UserID: u.ID}, nil).Once()
			f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()
			setup(f, u)

			err := f.svc.ResetPassword(context.Background(), "raw", "brand-new-password")

			assert.ErrorIs(t, err, boom)
		})
	}
}

func TestForgotPasswordEndpoint_SameAnswerForKnownAndUnknownEmails(t *testing.T) {
	known := newFixture(t)
	u := known.resetUser()
	known.users.EXPECT().GetByEmail(mock.Anything, mock.Anything, mock.Anything).Return(u, nil).Once()
	known.expectTx()
	known.tokens.EXPECT().Issue(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return("t", nil).Once()
	known.mail.EXPECT().Send(mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	unknown := newFixture(t)
	unknown.users.EXPECT().GetByEmail(mock.Anything, mock.Anything, mock.Anything).Return(user.User{}, apperr.NotFound()).Once()

	a := postJSON(newRouter(known), "/api/v1/auth/forgot-password", `{"email":"ann@example.com"}`)
	b := postJSON(newRouter(unknown), "/api/v1/auth/forgot-password", `{"email":"ghost@example.com"}`)

	assert.Equal(t, http.StatusOK, a.Code)
	assert.Equal(t, a.Code, b.Code)
	assert.Equal(t, a.Body.String(), b.Body.String(), "byte-identical responses: no user enumeration")
}

func TestForgotPasswordEndpoint_ValidatesInput(t *testing.T) {
	for _, body := range []string{`{}`, `{"email":"nope"}`, `nope`} {
		rec := postJSON(newRouter(newFixture(t)), "/api/v1/auth/forgot-password", body)

		assert.Equal(t, http.StatusBadRequest, rec.Code, body)
	}
}

func TestResetPasswordEndpoint(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		f := newFixture(t)
		u := f.resetUser()
		f.expectTx()
		f.tokens.EXPECT().Consume(mock.Anything, mock.Anything, "tok", auth.TokenPasswordReset).Return(auth.UserToken{UserID: u.ID}, nil).Once()
		f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()
		f.users.EXPECT().SetPasswordHash(mock.Anything, mock.Anything, u.ID, mock.Anything).Return(nil).Once()
		f.refresh.EXPECT().RevokeAllSessions(mock.Anything, mock.Anything, u.ID).Return(nil).Once()
		f.mail.EXPECT().Send(mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

		rec := postJSON(newRouter(f), "/api/v1/auth/reset-password", `{"token":"tok","password":"brand-new-password"}`)

		assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})

	t.Run("invalid token is a 400", func(t *testing.T) {
		f := newFixture(t)
		f.expectTx()
		f.tokens.EXPECT().Consume(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(auth.UserToken{}, auth.ErrInvalidToken()).Once()

		rec := postJSON(newRouter(f), "/api/v1/auth/reset-password", `{"token":"bad","password":"brand-new-password"}`)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), `"token"`)
	})

	t.Run("weak password is a 400 on the password field", func(t *testing.T) {
		rec := postJSON(newRouter(newFixture(t)), "/api/v1/auth/reset-password", `{"token":"tok","password":"short"}`)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), `"password"`)
	})

	t.Run("missing fields", func(t *testing.T) {
		rec := postJSON(newRouter(newFixture(t)), "/api/v1/auth/reset-password", `{"token":"tok"}`)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}
