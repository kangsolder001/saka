package auth_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
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

const goodPassword = "correct-horse"

func (f *fixture) activeUser(t *testing.T) user.User {
	t.Helper()
	return user.User{
		ID: uuid.New(), Email: "ann@example.com", Name: "Ann", Status: user.StatusActive,
		PasswordHash: mustHash(t, f.hasher, goodPassword),
	}
}

func mustHash(t *testing.T, h *auth.PasswordHasher, password string) string {
	t.Helper()
	hash, err := h.Hash(password)
	require.NoError(t, err)
	return hash
}

func (f *fixture) expectLookup(u user.User) {
	f.users.EXPECT().GetByEmail(mock.Anything, mock.Anything, "ann@example.com").Return(u, nil).Once()
}

func loginInput(password string) auth.LoginInput {
	return auth.LoginInput{Email: "ann@example.com", Password: password, Client: auth.ClientInfo{UserAgent: "UA", IP: "203.0.113.9"}}
}

func TestLogin_SuccessIssuesBothTokensForANewSession(t *testing.T) {
	f := newFixture(t)
	u := f.activeUser(t)
	family := uuid.New()
	refreshExpires := unitEpoch.Add(30 * 24 * time.Hour)
	f.expectLookup(u)
	f.expectTx()
	f.refresh.EXPECT().StartSession(mock.Anything, mock.Anything, u.ID, auth.ClientInfo{UserAgent: "UA", IP: "203.0.113.9"}).
		Return(auth.RefreshToken{Raw: "refresh-raw", FamilyID: family, ExpiresAt: refreshExpires}, nil).Once()

	res, err := f.svc.Login(context.Background(), loginInput(goodPassword))

	require.NoError(t, err)
	assert.Equal(t, u.ID, res.User.ID)
	assert.Equal(t, "refresh-raw", res.RefreshToken)
	assert.True(t, refreshExpires.Equal(res.RefreshExpiresAt))
	claims, err := f.jwt.Verify(res.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, u.ID, claims.UserID)
	assert.Equal(t, family, claims.SessionID, "the access token is bound to the refresh token family")
	assert.True(t, unitEpoch.Add(15*time.Minute).Equal(res.AccessExpiresAt))
}

func TestLogin_UnverifiedEmailMayLogIn(t *testing.T) {
	f := newFixture(t)
	u := f.activeUser(t) // EmailVerifiedAt is nil
	f.expectLookup(u)
	f.expectTx()
	f.refresh.EXPECT().StartSession(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(auth.RefreshToken{Raw: "r", FamilyID: uuid.New()}, nil).Once()

	_, err := f.svc.Login(context.Background(), loginInput(goodPassword))

	assert.NoError(t, err, "spec §5.2: unverified users can log in; only some actions are blocked")
}

func TestLogin_WrongPasswordAndUnknownEmailAreIndistinguishable(t *testing.T) {
	fWrong := newFixture(t)
	fWrong.expectLookup(fWrong.activeUser(t))
	fUnknown := newFixture(t)
	fUnknown.users.EXPECT().GetByEmail(mock.Anything, mock.Anything, "ann@example.com").
		Return(user.User{}, apperr.NotFound()).Once()

	_, wrongErr := fWrong.svc.Login(context.Background(), loginInput("not-the-password"))
	_, unknownErr := fUnknown.svc.Login(context.Background(), loginInput("not-the-password"))

	require.Error(t, wrongErr)
	require.Error(t, unknownErr)
	assert.True(t, apperr.IsCode(wrongErr, apperr.CodeUnauthenticated))
	assert.Equal(t, apperr.From(wrongErr).Code, apperr.From(unknownErr).Code)
	assert.Equal(t, apperr.From(wrongErr).Message, apperr.From(unknownErr).Message)
	assert.Equal(t, apperr.From(wrongErr).Details, apperr.From(unknownErr).Details)
	// neither started a session: no StartSession expectation was set
}

func TestLogin_SuspendedAccountWithCorrectPasswordIsForbidden(t *testing.T) {
	f := newFixture(t)
	u := f.activeUser(t)
	u.Status = user.StatusSuspended
	f.expectLookup(u)

	_, err := f.svc.Login(context.Background(), loginInput(goodPassword))

	assert.True(t, apperr.IsCode(err, apperr.CodeForbidden))
}

func TestLogin_SuspendedAccountWithWrongPasswordLooksLikeAnyFailedLogin(t *testing.T) {
	f := newFixture(t)
	u := f.activeUser(t)
	u.Status = user.StatusSuspended
	f.expectLookup(u)

	_, err := f.svc.Login(context.Background(), loginInput("wrong"))

	assert.True(t, apperr.IsCode(err, apperr.CodeUnauthenticated), "account state is only revealed to someone who knows the password")
}

func TestLogin_InfrastructureErrorsAreNotMaskedAsBadCredentials(t *testing.T) {
	f := newFixture(t)
	boom := errors.New("db down")
	f.users.EXPECT().GetByEmail(mock.Anything, mock.Anything, mock.Anything).Return(user.User{}, boom).Once()

	_, err := f.svc.Login(context.Background(), loginInput(goodPassword))

	assert.ErrorIs(t, err, boom)
}

func TestLogin_SessionStartFailureFailsTheLogin(t *testing.T) {
	f := newFixture(t)
	f.expectLookup(f.activeUser(t))
	f.expectTx()
	boom := errors.New("insert failed")
	f.refresh.EXPECT().StartSession(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(auth.RefreshToken{}, boom).Once()

	_, err := f.svc.Login(context.Background(), loginInput(goodPassword))

	assert.ErrorIs(t, err, boom)
}

func TestLogin_UpgradesPasswordHashesMadeWithWeakerParameters(t *testing.T) {
	f := newFixture(t)
	weak, err := auth.NewPasswordHasher(auth.Argon2Params{MemoryKiB: 32, Iterations: 1, Parallelism: 1})
	require.NoError(t, err)
	u := f.activeUser(t)
	u.PasswordHash = mustHash(t, weak, goodPassword)
	f.expectLookup(u)
	f.users.EXPECT().SetPasswordHash(mock.Anything, mock.Anything, u.ID, mock.MatchedBy(func(hash string) bool {
		ok, needsRehash, err := f.hasher.Verify(goodPassword, hash)
		return err == nil && ok && !needsRehash
	})).Return(nil).Once()
	f.expectTx()
	f.refresh.EXPECT().StartSession(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(auth.RefreshToken{Raw: "r", FamilyID: uuid.New()}, nil).Once()

	_, err = f.svc.Login(context.Background(), loginInput(goodPassword))

	assert.NoError(t, err)
}

func TestLogin_RehashFailureDoesNotFailTheLogin(t *testing.T) {
	f := newFixture(t)
	weak, err := auth.NewPasswordHasher(auth.Argon2Params{MemoryKiB: 32, Iterations: 1, Parallelism: 1})
	require.NoError(t, err)
	u := f.activeUser(t)
	u.PasswordHash = mustHash(t, weak, goodPassword)
	f.expectLookup(u)
	f.users.EXPECT().SetPasswordHash(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("write failed")).Once()
	f.expectTx()
	f.refresh.EXPECT().StartSession(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(auth.RefreshToken{Raw: "r", FamilyID: uuid.New()}, nil).Once()

	_, err = f.svc.Login(context.Background(), loginInput(goodPassword))

	assert.NoError(t, err)
}

func loginSetup(t *testing.T) (*fixture, uuid.UUID) {
	t.Helper()
	f := newFixture(t)
	u := f.activeUser(t)
	f.expectLookup(u)
	f.expectTx()
	f.refresh.EXPECT().StartSession(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(auth.RefreshToken{Raw: "the-refresh-token", FamilyID: uuid.New(), ExpiresAt: unitEpoch.Add(30 * 24 * time.Hour)}, nil).Once()
	return f, u.ID
}

func TestLoginEndpoint_DefaultDeliversRefreshTokenInHttpOnlyCookie(t *testing.T) {
	f, userID := loginSetup(t)

	rec := postJSON(newRouter(f), "/api/v1/auth/login", `{"email":"ann@example.com","password":"correct-horse"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `"access_token":"`)
	assert.Contains(t, body, `"token_type":"Bearer"`)
	assert.Contains(t, body, `"expires_in":900`)
	assert.Contains(t, body, userID.String())
	assert.NotContains(t, body, "the-refresh-token", "with cookie delivery the refresh token must stay out of the body (XSS)")
	assert.NotContains(t, body, "password")

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	c := cookies[0]
	assert.Equal(t, auth.RefreshCookieName, c.Name)
	assert.Equal(t, "the-refresh-token", c.Value)
	assert.True(t, c.HttpOnly)
	assert.Equal(t, http.SameSiteStrictMode, c.SameSite)
	assert.Equal(t, "/api/v1/auth", c.Path)
	assert.Equal(t, 30*24*3600, c.MaxAge)
	assert.False(t, c.Secure, "Secure is off here because HandlerConfig left it off (dev over http)")
}

func TestLoginEndpoint_SecureCookieWhenConfigured(t *testing.T) {
	f, _ := loginSetup(t)
	r := newRouterWith(f, auth.HandlerConfig{SecureCookie: true})

	rec := postJSON(r, "/api/v1/auth/login", `{"email":"ann@example.com","password":"correct-horse"}`)

	require.Len(t, rec.Result().Cookies(), 1)
	assert.True(t, rec.Result().Cookies()[0].Secure)
}

func TestLoginEndpoint_BodyDeliveryReturnsRefreshTokenAndNoCookie(t *testing.T) {
	f, _ := loginSetup(t)

	rec := postJSON(newRouter(f), "/api/v1/auth/login", `{"email":"ann@example.com","password":"correct-horse","token_delivery":"body"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"refresh_token":"the-refresh-token"`)
	assert.Empty(t, rec.Result().Cookies())
}

func TestLoginEndpoint_Rejections(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{"missing password", `{"email":"ann@example.com"}`, 400, "VALIDATION_ERROR"},
		{"bad email", `{"email":"nope","password":"x"}`, 400, "VALIDATION_ERROR"},
		{"unknown delivery", `{"email":"ann@example.com","password":"x","token_delivery":"carrier-pigeon"}`, 400, "VALIDATION_ERROR"},
		{"huge password", `{"email":"ann@example.com","password":"` + strings.Repeat("a", 2000) + `"}`, 400, "VALIDATION_ERROR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t) // no expectations: validation stops the request before the service

			rec := postJSON(newRouter(f), "/api/v1/auth/login", tt.body)

			assert.Equal(t, tt.status, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.code)
		})
	}
}

func TestLoginEndpoint_FailedLoginIs401WithGenericMessage(t *testing.T) {
	f := newFixture(t)
	f.expectLookup(f.activeUser(t))

	rec := postJSON(newRouter(f), "/api/v1/auth/login", `{"email":"ann@example.com","password":"wrong-password"}`)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"UNAUTHENTICATED"`)
	assert.Empty(t, rec.Result().Cookies())
}
