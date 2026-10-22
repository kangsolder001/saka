package auth_test

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

	"github.com/kangsolder001/saka/internal/auth"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/user"
)

func init() { gin.SetMode(gin.TestMode) }

func newRouter(f *fixture) *gin.Engine { return newRouterWith(f, auth.HandlerConfig{}) }

func newRouterWith(f *fixture, cfg auth.HandlerConfig) *gin.Engine {
	r := gin.New()
	auth.NewHandler(f.svc, auth.NewMiddleware(f.svc), cfg).Routes(r.Group("/api/v1"))
	return r
}

func postJSON(r http.Handler, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

const registerBody = `{"email":"ann@example.com","password":"correct-horse","name":"Ann","organization_name":"Acme"}`

func TestRegisterEndpoint_Created(t *testing.T) {
	f := newFixture(t)
	id := uuid.New()
	f.expectTx()
	f.users.EXPECT().Create(mock.Anything, mock.Anything, mock.Anything).
		Return(user.User{ID: id, Email: "ann@example.com", Name: "Ann"}, nil).Once()
	f.expectProvision()
	f.tokens.EXPECT().Issue(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return("t", nil).Once()
	f.mail.EXPECT().Send(mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

	rec := postJSON(newRouter(f), "/api/v1/auth/register", registerBody)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"user":{"id":"`+id.String()+`","email":"ann@example.com","name":"Ann","email_verified":false}}`, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "password")
	assert.NotContains(t, rec.Body.String(), "token", "register must not hand out tokens (spec §5.6)")
}

func TestRegisterEndpoint_Rejections(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
		wantField  string
	}{
		{"not json", `nope`, 400, "VALIDATION_ERROR", "body"},
		{"missing email", `{"password":"correct-horse","name":"Ann","organization_name":"Acme"}`, 400, "VALIDATION_ERROR", "email"},
		{"bad email", `{"email":"nope","password":"correct-horse","name":"Ann","organization_name":"Acme"}`, 400, "VALIDATION_ERROR", "email"},
		{"missing organization", `{"email":"a@b.co","password":"correct-horse","name":"Ann"}`, 400, "VALIDATION_ERROR", "organization_name"},
		{"unknown field", `{"email":"a@b.co","password":"correct-horse","name":"Ann","organization_name":"Acme","role":"admin"}`, 400, "VALIDATION_ERROR", "role"},
		{"short password", `{"email":"a@b.co","password":"short","name":"Ann","organization_name":"Acme"}`, 400, "VALIDATION_ERROR", "password"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t) // no expectations: nothing may reach the collaborators

			rec := postJSON(newRouter(f), "/api/v1/auth/register", tt.body)

			assert.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), `"code":"`+tt.wantCode+`"`)
			assert.Contains(t, rec.Body.String(), `"`+tt.wantField+`"`)
		})
	}
}

func TestRegisterEndpoint_DuplicateEmailIs409(t *testing.T) {
	f := newFixture(t)
	f.expectTx()
	f.users.EXPECT().Create(mock.Anything, mock.Anything, mock.Anything).
		Return(user.User{}, apperr.Conflict("Email sudah terdaftar.")).Once()

	rec := postJSON(newRouter(f), "/api/v1/auth/register", registerBody)

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"CONFLICT"`)
}
