package auth_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/auth"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/user"
)

func isRateLimited(err error) bool { return apperr.IsCode(err, apperr.CodeRateLimited) }

func TestRules_MatchSpecTable(t *testing.T) {
	assert.Equal(t, 5, auth.RuleLogin.Limit)
	assert.Equal(t, 15*time.Minute, auth.RuleLogin.Window)
	assert.Equal(t, 3, auth.RuleForgotPassword.Limit)
	assert.Equal(t, time.Hour, auth.RuleForgotPassword.Window)
	assert.Equal(t, 3, auth.RuleResendVerification.Limit)
	assert.Equal(t, time.Hour, auth.RuleResendVerification.Window)
	assert.Equal(t, 10, auth.RuleRegister.Limit)
	assert.Equal(t, time.Hour, auth.RuleRegister.Window)
}

func TestRegister_IsRateLimitedPerIPBeforeAnyWork(t *testing.T) {
	f := newFixture(t) // no collaborator expectations: nothing may run when over the limit
	f.limiter.denyTake = true

	_, err := f.svc.Register(context.Background(), validRegistration())

	require.True(t, isRateLimited(err))
	assert.Equal(t, 90, apperr.From(err).Details[apperr.DetailRetryAfterSeconds])
	assert.Equal(t, []string{"register"}, f.limiter.takes)
}

func TestForgotPassword_IsRateLimitedBeforeLookingTheUserUp(t *testing.T) {
	f := newFixture(t) // no GetByEmail expectation: the limit must not depend on the account existing
	f.limiter.denyTake = true

	err := f.svc.ForgotPassword(context.Background(), "ghost@example.com")

	assert.True(t, isRateLimited(err))
	assert.Equal(t, []string{"forgot_password"}, f.limiter.takes)
}

func TestResendVerification_IsRateLimitedPerUser(t *testing.T) {
	f := newFixture(t)
	f.limiter.denyTake = true

	err := f.svc.ResendVerification(context.Background(), uuid.New())

	assert.True(t, isRateLimited(err))
	assert.Equal(t, []string{"resend_verification"}, f.limiter.takes)
}

func TestLogin_BlockedWhenTooManyFailuresEvenWithTheRightPassword(t *testing.T) {
	f := newFixture(t) // no GetByEmail expectation: a blocked client never reaches the password check
	f.limiter.denyCheck = true

	_, err := f.svc.Login(context.Background(), loginInput(goodPassword))

	require.True(t, isRateLimited(err))
	assert.Empty(t, f.limiter.records, "blocked attempts are not counted again")
}

func TestLogin_CountsFailuresAndResetsOnSuccess(t *testing.T) {
	t.Run("wrong password is a counted failure", func(t *testing.T) {
		f := newFixture(t)
		f.expectLookup(f.activeUser(t))

		_, err := f.svc.Login(context.Background(), loginInput("wrong"))

		require.Error(t, err)
		assert.Equal(t, []string{"login"}, f.limiter.records)
		assert.Zero(t, f.limiter.resets)
	})

	t.Run("unknown email is counted exactly like a wrong password", func(t *testing.T) {
		f := newFixture(t)
		f.users.EXPECT().GetByEmail(mock.Anything, mock.Anything, mock.Anything).Return(user.User{}, apperr.NotFound()).Once()

		_, err := f.svc.Login(context.Background(), loginInput("whatever"))

		require.Error(t, err)
		assert.Equal(t, []string{"login"}, f.limiter.records, "otherwise rate limiting would reveal which emails exist")
	})

	t.Run("success clears the failure count", func(t *testing.T) {
		f, _ := loginSetup(t)

		_, err := f.svc.Login(context.Background(), loginInput(goodPassword))

		require.NoError(t, err)
		assert.Equal(t, 1, f.limiter.resets)
		assert.Empty(t, f.limiter.records)
	})
}

func TestEndpoints_ReturnTooManyRequestsWithRetryAfter(t *testing.T) {
	tests := map[string]struct {
		path string
		body string
		deny func(l *fakeLimiter)
	}{
		"register": {"/api/v1/auth/register", registerBody, func(l *fakeLimiter) { l.denyTake = true }},
		"login":    {"/api/v1/auth/login", `{"email":"ann@example.com","password":"x"}`, func(l *fakeLimiter) { l.denyCheck = true }},
		"forgot":   {"/api/v1/auth/forgot-password", `{"email":"ann@example.com"}`, func(l *fakeLimiter) { l.denyTake = true }},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			tt.deny(f.limiter)

			rec := postJSON(newRouter(f), tt.path, tt.body)

			assert.Equal(t, http.StatusTooManyRequests, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), `"code":"RATE_LIMITED"`)
			assert.Equal(t, "90", rec.Header().Get("Retry-After"))
		})
	}
}
