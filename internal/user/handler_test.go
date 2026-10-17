package user_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/httpx"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
	"github.com/kangsolder001/saka/internal/user"
	usermocks "github.com/kangsolder001/saka/internal/user/mocks"
)

func init() { gin.SetMode(gin.TestMode) }

// asUser stands in for auth's middleware: it puts the user and session IDs on
// the request context.
func asUser(id, session uuid.UUID) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := reqctx.WithUserID(c.Request.Context(), id)
		c.Request = c.Request.WithContext(reqctx.WithSessionID(ctx, session))
		c.Next()
	}
}

type handlerFixture struct {
	router   *gin.Engine
	profiles *usermocks.MockProfileService
	security *usermocks.MockAccountSecurity
	userID   uuid.UUID
	session  uuid.UUID
}

func newHandlerFixture(t *testing.T) *handlerFixture {
	t.Helper()
	f := &handlerFixture{
		profiles: usermocks.NewMockProfileService(t),
		security: usermocks.NewMockAccountSecurity(t),
		userID:   uuid.New(),
		session:  uuid.New(),
	}
	f.router = gin.New()
	user.NewHandler(user.Dependencies{Profiles: f.profiles, Security: f.security, Pool: stubDB{}}).
		Routes(f.router.Group("/api/v1"), asUser(f.userID, f.session))
	return f
}

func (f *handlerFixture) do(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func TestGetMe_ReturnsTheCallersProfile(t *testing.T) {
	f := newHandlerFixture(t)
	avatar := "https://cdn.example.com/ann.png"
	f.profiles.EXPECT().GetSummary(mock.Anything, mock.Anything, f.userID).
		Return(user.Summary{ID: f.userID, Email: "ann@example.com", Name: "Ann", AvatarURL: &avatar, Status: user.StatusActive}, nil).Once()

	rec := f.do(http.MethodGet, "/api/v1/me", "")

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{
		"user": {"id":"`+f.userID.String()+`","email":"ann@example.com","name":"Ann","avatar_url":"https://cdn.example.com/ann.png","email_verified":false},
		"organizations": []
	}`, rec.Body.String())
}

func TestGetMe_AvatarIsNullWhenUnset(t *testing.T) {
	f := newHandlerFixture(t)
	f.profiles.EXPECT().GetSummary(mock.Anything, mock.Anything, f.userID).
		Return(user.Summary{ID: f.userID, Email: "ann@example.com", Name: "Ann"}, nil).Once()

	rec := f.do(http.MethodGet, "/api/v1/me", "")

	assert.Contains(t, rec.Body.String(), `"avatar_url":null`)
	assert.NotContains(t, rec.Body.String(), "password")
}

func TestGetMe_UnknownUserIsNotFound(t *testing.T) {
	f := newHandlerFixture(t)
	f.profiles.EXPECT().GetSummary(mock.Anything, mock.Anything, f.userID).Return(user.Summary{}, apperr.NotFound()).Once()

	rec := f.do(http.MethodGet, "/api/v1/me", "")

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestPatchMe_PassesOnlyTheFieldsThatWereSent(t *testing.T) {
	name := "Ann Lee"
	tests := []struct {
		name string
		body string
		want user.ProfileUpdate
	}{
		{"name only", `{"name":"Ann Lee"}`, user.ProfileUpdate{Name: &name}},
		{"avatar only", `{"avatar_url":"https://cdn.example.com/a.png"}`, user.ProfileUpdate{SetAvatar: true, AvatarURL: ptr("https://cdn.example.com/a.png")}},
		{"avatar removed with null", `{"avatar_url":null}`, user.ProfileUpdate{SetAvatar: true}},
		{"both", `{"name":"Ann Lee","avatar_url":null}`, user.ProfileUpdate{Name: &name, SetAvatar: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newHandlerFixture(t)
			f.profiles.EXPECT().UpdateProfile(mock.Anything, mock.Anything, f.userID, tt.want).
				Return(user.User{ID: f.userID, Email: "ann@example.com", Name: "Ann Lee"}, nil).Once()

			rec := f.do(http.MethodPatch, "/api/v1/me", tt.body)

			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), `"name":"Ann Lee"`)
		})
	}
}

func TestPatchMe_RejectsMalformedBodies(t *testing.T) {
	tests := map[string]string{
		"unknown field":                  `{"email":"x@example.com"}`,
		"cannot change status":           `{"status":"active"}`,
		"name of the wrong type":         `{"name":5}`,
		"avatar of the wrong type":       `{"avatar_url":5}`,
		"not json":                       `nope`,
		"empty body":                     ``,
		"trailing data after the object": `{"name":"A"} {}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			f := newHandlerFixture(t) // the mock has no expectation: the service must not be reached

			rec := f.do(http.MethodPatch, "/api/v1/me", body)

			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), "VALIDATION_ERROR")
		})
	}
}

func TestPatchMe_ServiceValidationErrorsReachTheClient(t *testing.T) {
	f := newHandlerFixture(t)
	f.profiles.EXPECT().UpdateProfile(mock.Anything, mock.Anything, f.userID, mock.Anything).
		Return(user.User{}, apperr.Validation(map[string]string{"name": "is required"})).Once()

	rec := f.do(http.MethodPatch, "/api/v1/me", `{"name":" "}`)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"name":"is required"`)
}

func TestMe_RequiresAnAuthenticatedUser(t *testing.T) {
	// Mounted with a middleware that authenticates nobody: a wiring bug must fail closed.
	r := gin.New()
	user.NewHandler(user.Dependencies{
		Profiles: usermocks.NewMockProfileService(t), Security: usermocks.NewMockAccountSecurity(t), Pool: stubDB{},
	}).Routes(r.Group("/api/v1"), func(c *gin.Context) { c.Next() })

	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/me", ""},
		{http.MethodPatch, "/api/v1/me", `{"name":"X"}`},
		{http.MethodPost, "/api/v1/me/password", `{"current_password":"a","new_password":"b"}`},
		{http.MethodPost, "/api/v1/me/email", `{"new_email":"a@example.com","current_password":"b"}`},
	} {
		req := httptest.NewRequestWithContext(context.Background(), c.method, c.path, strings.NewReader(c.body))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code, c.method+" "+c.path)
	}
}

func TestChangePassword_PassesTheCallerAndTheirCurrentSession(t *testing.T) {
	f := newHandlerFixture(t)
	f.security.EXPECT().ChangePassword(mock.Anything, f.userID, f.session, "old-password", "new-password-1").Return(nil).Once()

	rec := f.do(http.MethodPost, "/api/v1/me/password", `{"current_password":"old-password","new_password":"new-password-1"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "password-1", "the response never echoes passwords")
}

func TestChangePassword_RejectsBadBodiesWithoutCallingTheService(t *testing.T) {
	tests := map[string]string{
		"missing current":  `{"new_password":"new-password-1"}`,
		"missing new":      `{"current_password":"old-password"}`,
		"unknown field":    `{"current_password":"a","new_password":"b","user_id":"x"}`,
		"huge new":         `{"current_password":"a","new_password":"` + strings.Repeat("a", 1025) + `"}`,
		"wrong field name": `{"password":"a","new_password":"b"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			f := newHandlerFixture(t)

			rec := f.do(http.MethodPost, "/api/v1/me/password", body)

			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		})
	}
}

func TestChangePassword_ServiceErrorsReachTheClient(t *testing.T) {
	f := newHandlerFixture(t)
	f.security.EXPECT().ChangePassword(mock.Anything, f.userID, f.session, mock.Anything, mock.Anything).
		Return(apperr.Validation(map[string]string{"current_password": "is incorrect"})).Once()

	rec := f.do(http.MethodPost, "/api/v1/me/password", `{"current_password":"wrong","new_password":"new-password-1"}`)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"current_password":"is incorrect"`)
}

func TestRequestEmailChange_PassesTheCallerThePasswordAndTheNewAddress(t *testing.T) {
	f := newHandlerFixture(t)
	f.security.EXPECT().RequestEmailChange(mock.Anything, f.userID, "my-password", "New@Example.com").Return(nil).Once()

	rec := f.do(http.MethodPost, "/api/v1/me/email", `{"new_email":"New@Example.com","current_password":"my-password"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "my-password")
}

func TestRequestEmailChange_RejectsBadBodiesWithoutCallingTheService(t *testing.T) {
	tests := map[string]string{
		"missing password": `{"new_email":"a@example.com"}`,
		"missing email":    `{"current_password":"x"}`,
		"not an email":     `{"new_email":"nope","current_password":"x"}`,
		"unknown field":    `{"new_email":"a@example.com","current_password":"x","user_id":"y"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			f := newHandlerFixture(t)

			rec := f.do(http.MethodPost, "/api/v1/me/email", body)

			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		})
	}
}

func TestRequestEmailChange_ServiceErrorsReachTheClient(t *testing.T) {
	tests := map[string]struct {
		err    error
		status int
	}{
		"taken":      {apperr.Conflict("Email sudah terdaftar."), http.StatusConflict},
		"rate limit": {apperr.RateLimitedAfter(time.Minute), http.StatusTooManyRequests},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := newHandlerFixture(t)
			f.security.EXPECT().RequestEmailChange(mock.Anything, f.userID, mock.Anything, mock.Anything).Return(tt.err).Once()

			rec := f.do(http.MethodPost, "/api/v1/me/email", `{"new_email":"a@example.com","current_password":"x"}`)

			assert.Equal(t, tt.status, rec.Code)
		})
	}
}

// The routes are mounted with a middleware that rejects everyone: confirming
// must still work, because it does not sit behind it.
func TestConfirmEmailChange_NeedsNoLoginBecauseTheTokenIsTheCredential(t *testing.T) {
	security := usermocks.NewMockAccountSecurity(t)
	security.EXPECT().ConfirmEmailChange(mock.Anything, "raw-token").Return("new@example.com", nil).Once()
	r := gin.New()
	user.NewHandler(user.Dependencies{Profiles: usermocks.NewMockProfileService(t), Security: security, Pool: stubDB{}}).
		Routes(r.Group("/api/v1"), func(c *gin.Context) { httpx.Error(c, apperr.Unauthenticated()) })

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/me/email/confirm", strings.NewReader(`{"token":"raw-token"}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"email":"new@example.com","email_verified":true}`, rec.Body.String())
}

func TestConfirmEmailChange_RejectsBadBodiesAndBadTokens(t *testing.T) {
	f := newHandlerFixture(t)
	f.security.EXPECT().ConfirmEmailChange(mock.Anything, "used-or-expired").Return("", apperr.Validation(map[string]string{"token": "invalid or expired"})).Once()

	assert.Equal(t, http.StatusBadRequest, f.do(http.MethodPost, "/api/v1/me/email/confirm", `{}`).Code, "token is required")
	assert.Equal(t, http.StatusBadRequest, f.do(http.MethodPost, "/api/v1/me/email/confirm", `{"token":"`+strings.Repeat("a", 257)+`"}`).Code)
	rec := f.do(http.MethodPost, "/api/v1/me/email/confirm", `{"token":"used-or-expired"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func ptr[T any](v T) *T { return &v }
