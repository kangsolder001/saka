package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/httpx"
)

type loginReq struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
}

func bindEngine() *gin.Engine {
	r := gin.New()
	r.POST("/login", func(c *gin.Context) {
		var req loginReq
		if !httpx.BindJSON(c, &req) {
			return
		}
		c.JSON(http.StatusOK, gin.H{"email": req.Email})
	})
	return r
}

func post(r http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestBindJSON_Valid(t *testing.T) {
	rec := post(bindEngine(), `{"email":"a@b.co","password":"longenough"}`)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"email":"a@b.co"}`, rec.Body.String())
}

func TestBindJSON_Rejects(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantField string
		wantMsg   string
	}{
		{"empty body", ``, "body", "is required"},
		{"malformed json", `{"email":`, "body", "is not valid JSON"},
		{"not json", `hello`, "body", "is not valid JSON"},
		{"wrong type", `{"email":123,"password":"longenough"}`, "email", "has an invalid type"},
		{"unknown field", `{"email":"a@b.co","password":"longenough","admin":true}`, "admin", "is an unknown field"},
		{"trailing data", `{"email":"a@b.co","password":"longenough"} {}`, "body", "must contain a single JSON object"},
		{"failed validation", `{"email":"nope","password":"longenough"}`, "email", "must be a valid email address"},
		{"missing field", `{"email":"a@b.co"}`, "password", "is required"},
		{"too large", `{"email":"` + strings.Repeat("a", httpx.MaxBodyBytes) + `"}`, "body", "is too large"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := post(bindEngine(), tt.body)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Contains(t, rec.Body.String(), `"code":"VALIDATION_ERROR"`)
			assert.Contains(t, rec.Body.String(), `"`+tt.wantField+`":"`+tt.wantMsg+`"`)
		})
	}
}

func TestBindJSON_HandlerDoesNotRunOnFailure(t *testing.T) {
	r := gin.New()
	ran := false
	r.POST("/x", func(c *gin.Context) {
		var req loginReq
		if !httpx.BindJSON(c, &req) {
			return
		}
		ran = true
	})

	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{}`))
	r.ServeHTTP(httptest.NewRecorder(), req)

	assert.False(t, ran)
}
