package user_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
	"github.com/kangsolder001/saka/internal/user"
	usermocks "github.com/kangsolder001/saka/internal/user/mocks"
)

func init() { gin.SetMode(gin.TestMode) }

// asUser stands in for auth's middleware: it puts a user ID on the request context.
func asUser(id uuid.UUID) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(reqctx.WithUserID(c.Request.Context(), id))
		c.Next()
	}
}

type handlerFixture struct {
	router   *gin.Engine
	profiles *usermocks.MockProfileService
	userID   uuid.UUID
}

func newHandlerFixture(t *testing.T) *handlerFixture {
	t.Helper()
	f := &handlerFixture{profiles: usermocks.NewMockProfileService(t), userID: uuid.New()}
	f.router = gin.New()
	user.NewHandler(f.profiles, stubDB{}).Routes(f.router.Group("/api/v1"), asUser(f.userID))
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
	user.NewHandler(usermocks.NewMockProfileService(t), stubDB{}).Routes(r.Group("/api/v1"), func(c *gin.Context) { c.Next() })

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/me", http.NoBody)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func ptr[T any](v T) *T { return &v }
