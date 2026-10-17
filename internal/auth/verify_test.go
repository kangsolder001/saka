package auth_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/auth"
	"github.com/kangsolder001/saka/internal/mail"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/user"
)

func TestVerifyEmail_ConsumesTokenThenMarksUserVerified(t *testing.T) {
	f := newFixture(t)
	userID := uuid.New()
	f.expectTx()
	f.tokens.EXPECT().Consume(mock.Anything, mock.Anything, "the-raw-token", auth.TokenEmailVerification).
		Return(auth.UserToken{UserID: userID}, nil).Once()
	f.users.EXPECT().MarkEmailVerified(mock.Anything, mock.Anything, userID).Return(nil).Once()

	err := f.svc.VerifyEmail(context.Background(), "the-raw-token")

	assert.NoError(t, err)
}

func TestVerifyEmail_InvalidTokenDoesNotTouchTheUser(t *testing.T) {
	f := newFixture(t)
	f.expectTx()
	f.tokens.EXPECT().Consume(mock.Anything, mock.Anything, "bad", auth.TokenEmailVerification).
		Return(auth.UserToken{}, auth.ErrInvalidToken()).Once()

	err := f.svc.VerifyEmail(context.Background(), "bad")

	assert.True(t, apperr.IsCode(err, apperr.CodeValidation))
	// no MarkEmailVerified expectation: it must not be called
}

func TestVerifyEmail_RollsBackTheTokenWhenMarkingFails(t *testing.T) {
	f := newFixture(t)
	f.expectTx()
	f.tokens.EXPECT().Consume(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(auth.UserToken{UserID: uuid.New()}, nil).Once()
	boom := errors.New("db down")
	f.users.EXPECT().MarkEmailVerified(mock.Anything, mock.Anything, mock.Anything).Return(boom).Once()

	err := f.svc.VerifyEmail(context.Background(), "t")

	assert.ErrorIs(t, err, boom, "returned from the transaction, so the token use is rolled back")
}

func TestResendVerification_QueuesNewEmailForUnverifiedUser(t *testing.T) {
	f := newFixture(t)
	u := user.User{ID: uuid.New(), Email: "ann@example.com", Name: "Ann"}
	f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()
	f.expectTx()
	f.tokens.EXPECT().Issue(mock.Anything, mock.Anything, u.ID, auth.TokenEmailVerification, nil).Return("fresh", nil).Once()
	f.mail.EXPECT().Send(mock.Anything, mock.Anything, mail.Email{
		Template: "verify_email", To: "ann@example.com",
		Data: map[string]any{"name": "Ann", "url": "https://app.example.com/verify-email?token=fresh"},
	}).Return(nil).Once()

	require.NoError(t, f.svc.ResendVerification(context.Background(), u.ID))
}

func TestResendVerification_AlreadyVerifiedIsConflict(t *testing.T) {
	f := newFixture(t)
	verifiedAt := unitEpoch
	u := user.User{ID: uuid.New(), EmailVerifiedAt: &verifiedAt}
	f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()

	err := f.svc.ResendVerification(context.Background(), u.ID)

	assert.True(t, apperr.IsCode(err, apperr.CodeConflict))
}

func TestResendVerification_UnknownUser(t *testing.T) {
	f := newFixture(t)
	f.users.EXPECT().GetByID(mock.Anything, mock.Anything, mock.Anything).Return(user.User{}, apperr.NotFound()).Once()

	err := f.svc.ResendVerification(context.Background(), uuid.New())

	assert.True(t, apperr.IsCode(err, apperr.CodeNotFound))
}

func TestVerifyEmailEndpoint(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		f := newFixture(t)
		f.expectTx()
		f.tokens.EXPECT().Consume(mock.Anything, mock.Anything, "tok", auth.TokenEmailVerification).
			Return(auth.UserToken{UserID: uuid.New()}, nil).Once()
		f.users.EXPECT().MarkEmailVerified(mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

		rec := postJSON(newRouter(f), "/api/v1/auth/verify-email", `{"token":"tok"}`)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"email_verified":true}`, rec.Body.String())
	})

	t.Run("invalid token is a 400", func(t *testing.T) {
		f := newFixture(t)
		f.expectTx()
		f.tokens.EXPECT().Consume(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(auth.UserToken{}, auth.ErrInvalidToken()).Once()

		rec := postJSON(newRouter(f), "/api/v1/auth/verify-email", `{"token":"bad"}`)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), `"token":"invalid or expired"`)
	})

	t.Run("missing token", func(t *testing.T) {
		rec := postJSON(newRouter(newFixture(t)), "/api/v1/auth/verify-email", `{}`)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), `"token"`)
	})
}
