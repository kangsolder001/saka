package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/auth"
	"github.com/kangsolder001/saka/internal/user"
)

func TestLogout_RevokesTheCallersSessionOnly(t *testing.T) {
	f := newFixture(t)
	session := uuid.New()
	f.refresh.EXPECT().RevokeSession(mock.Anything, mock.Anything, session).Return(nil).Once()

	err := f.svc.Logout(context.Background(), auth.Principal{UserID: uuid.New(), SessionID: session})

	assert.NoError(t, err)
	// RevokeAllSessions has no expectation: other devices must stay logged in
}

func TestLogout_PropagatesStoreErrors(t *testing.T) {
	f := newFixture(t)
	boom := errors.New("db down")
	f.refresh.EXPECT().RevokeSession(mock.Anything, mock.Anything, mock.Anything).Return(boom).Once()

	err := f.svc.Logout(context.Background(), auth.Principal{SessionID: uuid.New()})

	assert.ErrorIs(t, err, boom)
}

func logoutRequest(r http.Handler, authorization string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/logout", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestLogoutEndpoint_RevokesSessionAndClearsCookie(t *testing.T) {
	f := newFixture(t)
	u := user.Summary{ID: uuid.New(), Status: user.StatusActive}
	session := uuid.New()
	f.users.EXPECT().GetSummary(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()
	f.refresh.EXPECT().SessionActive(mock.Anything, mock.Anything, u.ID, session).Return(true, nil).Once()
	f.refresh.EXPECT().RevokeSession(mock.Anything, mock.Anything, session).Return(nil).Once()

	rec := logoutRequest(newRouter(f), "Bearer "+f.accessToken(t, u.ID, session))

	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Empty(t, rec.Body.String())
	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, auth.RefreshCookieName, cookies[0].Name)
	assert.Negative(t, cookies[0].MaxAge)
}

func TestLogoutEndpoint_RequiresLogin(t *testing.T) {
	f := newFixture(t) // no expectations: nothing is revoked without authentication

	rec := logoutRequest(newRouter(f), "")

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
