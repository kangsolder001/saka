package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/auth"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
	"github.com/kangsolder001/saka/internal/user"
)

func (f *fixture) accessToken(t *testing.T, userID, sessionID uuid.UUID) string {
	t.Helper()
	token, _, err := f.jwt.Sign(userID, sessionID)
	require.NoError(t, err)
	return token
}

func TestAuthenticate_ValidTokenForActiveUserAndLiveSession(t *testing.T) {
	f := newFixture(t)
	verifiedAt := unitEpoch
	u := user.Summary{ID: uuid.New(), Status: user.StatusActive, EmailVerifiedAt: &verifiedAt}
	session := uuid.New()
	f.users.EXPECT().GetSummary(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()
	f.refresh.EXPECT().SessionActive(mock.Anything, mock.Anything, u.ID, session).Return(true, nil).Once()

	p, err := f.svc.Authenticate(context.Background(), f.accessToken(t, u.ID, session))

	require.NoError(t, err)
	assert.Equal(t, auth.Principal{UserID: u.ID, SessionID: session, EmailVerified: true}, p)
}

func TestAuthenticate_Rejections(t *testing.T) {
	userID, session := uuid.New(), uuid.New()
	tests := []struct {
		name     string
		setup    func(f *fixture)
		token    func(f *fixture, t *testing.T) string
		wantCode apperr.Code
	}{
		{
			name:     "garbage token",
			token:    func(*fixture, *testing.T) string { return "garbage" },
			wantCode: apperr.CodeUnauthenticated,
		},
		{
			name: "expired token",
			token: func(f *fixture, t *testing.T) string {
				tok := f.accessToken(t, userID, session)
				f.clock.Advance(16 * time.Minute)
				return tok
			},
			wantCode: apperr.CodeTokenExpired,
		},
		{
			name: "user no longer exists",
			setup: func(f *fixture) {
				f.users.EXPECT().GetSummary(mock.Anything, mock.Anything, userID).Return(user.Summary{}, apperr.NotFound()).Once()
			},
			wantCode: apperr.CodeUnauthenticated,
		},
		{
			name: "user suspended",
			setup: func(f *fixture) {
				f.users.EXPECT().GetSummary(mock.Anything, mock.Anything, userID).
					Return(user.Summary{ID: userID, Status: user.StatusSuspended}, nil).Once()
			},
			wantCode: apperr.CodeUnauthenticated,
		},
		{
			name: "session revoked or expired",
			setup: func(f *fixture) {
				f.users.EXPECT().GetSummary(mock.Anything, mock.Anything, userID).
					Return(user.Summary{ID: userID, Status: user.StatusActive}, nil).Once()
				f.refresh.EXPECT().SessionActive(mock.Anything, mock.Anything, userID, session).Return(false, nil).Once()
			},
			wantCode: apperr.CodeUnauthenticated,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			if tt.setup != nil {
				tt.setup(f)
			}
			token := f.accessToken(t, userID, session)
			if tt.token != nil {
				token = tt.token(f, t)
			}

			_, err := f.svc.Authenticate(context.Background(), token)

			require.Error(t, err)
			assert.True(t, apperr.IsCode(err, tt.wantCode), "got %v", err)
		})
	}
}

func TestAuthenticate_InfrastructureErrorsAreNotTurnedIntoUnauthenticated(t *testing.T) {
	f := newFixture(t)
	boom := errors.New("db down")
	f.users.EXPECT().GetSummary(mock.Anything, mock.Anything, mock.Anything).Return(user.Summary{}, boom).Once()

	_, err := f.svc.Authenticate(context.Background(), f.accessToken(t, uuid.New(), uuid.New()))

	assert.ErrorIs(t, err, boom, "a database outage must be a 500, not a forced logout")
}

// protectedRouter mounts the middleware on a probe route that reports what it saw.
func protectedRouter(f *fixture, guards ...gin.HandlerFunc) *gin.Engine {
	mw := auth.NewMiddleware(f.svc)
	r := gin.New()
	chain := append([]gin.HandlerFunc{mw.Authenticate()}, guards...)
	r.GET("/probe", append(chain, func(c *gin.Context) {
		p, _ := auth.PrincipalFrom(c)
		uid, _ := reqctx.UserID(c.Request.Context())
		sid, _ := reqctx.SessionID(c.Request.Context())
		c.JSON(http.StatusOK, gin.H{"principal": p.UserID, "ctx_user": uid, "ctx_session": sid})
	})...)
	return r
}

func getWith(r http.Handler, authorization string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/probe", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestAuthenticateMiddleware_SetsPrincipalAndRequestContext(t *testing.T) {
	f := newFixture(t)
	u := user.Summary{ID: uuid.New(), Status: user.StatusActive}
	session := uuid.New()
	f.users.EXPECT().GetSummary(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()
	f.refresh.EXPECT().SessionActive(mock.Anything, mock.Anything, u.ID, session).Return(true, nil).Once()

	rec := getWith(protectedRouter(f), "Bearer "+f.accessToken(t, u.ID, session))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"principal":"`+u.ID.String()+`"`)
	assert.Contains(t, rec.Body.String(), `"ctx_user":"`+u.ID.String()+`"`, "services and logs read the user from the context")
	assert.Contains(t, rec.Body.String(), `"ctx_session":"`+session.String()+`"`)
}

func TestAuthenticateMiddleware_AcceptsLowercaseScheme(t *testing.T) {
	f := newFixture(t)
	u := user.Summary{ID: uuid.New(), Status: user.StatusActive}
	session := uuid.New()
	f.users.EXPECT().GetSummary(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()
	f.refresh.EXPECT().SessionActive(mock.Anything, mock.Anything, u.ID, session).Return(true, nil).Once()

	rec := getWith(protectedRouter(f), "bearer "+f.accessToken(t, u.ID, session))

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAuthenticateMiddleware_RejectsMissingOrMalformedHeaders(t *testing.T) {
	for name, header := range map[string]string{
		"missing":          "",
		"wrong scheme":     "Basic dXNlcjpwYXNz",
		"scheme only":      "Bearer",
		"empty token":      "Bearer ",
		"extra parts":      "Bearer a b",
		"no scheme":        "justatoken",
		"token in the raw": "Token abc",
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t) // no expectations: nothing may reach the service

			rec := getWith(protectedRouter(f), header)

			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.Contains(t, rec.Body.String(), `"code":"UNAUTHENTICATED"`)
		})
	}
}

func TestAuthenticateMiddleware_ExpiredTokenIsTokenExpired(t *testing.T) {
	f := newFixture(t)
	token := f.accessToken(t, uuid.New(), uuid.New())
	f.clock.Advance(time.Hour)

	rec := getWith(protectedRouter(f), "Bearer "+token)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"TOKEN_EXPIRED"`)
}

func TestRequireVerifiedEmail(t *testing.T) {
	verifiedAt := unitEpoch
	tests := []struct {
		name     string
		verified *time.Time
		status   int
		code     string
	}{
		{"unverified is blocked", nil, http.StatusForbidden, "EMAIL_NOT_VERIFIED"},
		{"verified passes", &verifiedAt, http.StatusOK, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			u := user.Summary{ID: uuid.New(), Status: user.StatusActive, EmailVerifiedAt: tt.verified}
			session := uuid.New()
			f.users.EXPECT().GetSummary(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()
			f.refresh.EXPECT().SessionActive(mock.Anything, mock.Anything, u.ID, session).Return(true, nil).Once()
			guard := auth.NewMiddleware(f.svc).RequireVerifiedEmail()

			rec := getWith(protectedRouter(f, guard), "Bearer "+f.accessToken(t, u.ID, session))

			assert.Equal(t, tt.status, rec.Code)
			if tt.code != "" {
				assert.Contains(t, rec.Body.String(), `"code":"`+tt.code+`"`)
			}
		})
	}
}

func TestRequireVerifiedEmail_WithoutAuthenticateFailsClosed(t *testing.T) {
	f := newFixture(t)
	r := gin.New()
	r.GET("/probe", auth.NewMiddleware(f.svc).RequireVerifiedEmail(), func(c *gin.Context) { c.Status(http.StatusOK) })

	rec := getWith(r, "")

	assert.Equal(t, http.StatusUnauthorized, rec.Code, "a guard mounted without Authenticate must deny, never allow")
}

func TestResendVerificationEndpoint_RequiresLogin(t *testing.T) {
	f := newFixture(t)

	rec := postJSON(newRouter(f), "/api/v1/auth/resend-verification", ``)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
