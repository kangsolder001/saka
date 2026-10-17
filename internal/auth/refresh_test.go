package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/auth"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/user"
)

var client = auth.ClientInfo{UserAgent: "UA", IP: "203.0.113.9"}

func (f *fixture) expectRotate(raw string, result auth.RotateResult) {
	f.expectTx()
	f.refresh.EXPECT().Rotate(mock.Anything, mock.Anything, raw, client).Return(result, nil).Once()
}

func TestRefresh_RotatesAndIssuesNewTokenPair(t *testing.T) {
	f := newFixture(t)
	u := user.User{ID: uuid.New(), Status: user.StatusActive}
	family := uuid.New()
	expires := unitEpoch.Add(30 * 24 * time.Hour)
	f.expectRotate("old", auth.RotateResult{
		Outcome: auth.Rotated, UserID: u.ID,
		Token: auth.RefreshToken{Raw: "new", FamilyID: family, ExpiresAt: expires},
	})
	f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()

	res, err := f.svc.Refresh(context.Background(), "old", client)

	require.NoError(t, err)
	assert.Equal(t, "new", res.RefreshToken)
	assert.True(t, expires.Equal(res.RefreshExpiresAt))
	claims, err := f.jwt.Verify(res.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, u.ID, claims.UserID)
	assert.Equal(t, family, claims.SessionID, "rotation keeps the session: same family")
}

func TestRefresh_UnknownAndExpiredTokensAreUnauthenticated(t *testing.T) {
	for name, outcome := range map[string]auth.RotateOutcome{"unknown": auth.Unknown, "expired": auth.Expired} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.expectRotate("tok", auth.RotateResult{Outcome: outcome})

			_, err := f.svc.Refresh(context.Background(), "tok", client)

			assert.True(t, apperr.IsCode(err, apperr.CodeUnauthenticated))
		})
	}
}

func TestRefresh_ReuseIsRejectedButTheTransactionStillCommits(t *testing.T) {
	f := newFixture(t)
	txResult := errors.New("not run")
	f.tx.EXPECT().WithTx(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, fn func(tx pgx.Tx) error) error {
			txResult = fn(nil)
			return txResult
		}).Once()
	f.refresh.EXPECT().Rotate(mock.Anything, mock.Anything, "stolen", client).
		Return(auth.RotateResult{Outcome: auth.Reused}, nil).Once()

	_, err := f.svc.Refresh(context.Background(), "stolen", client)

	assert.True(t, apperr.IsCode(err, apperr.CodeUnauthenticated), "the thief is rejected")
	assert.NoError(t, txResult, "the transaction function returned nil, so the family revocation is committed, not rolled back")
}

func TestRefresh_InactiveOrMissingUserIsUnauthenticated(t *testing.T) {
	tests := map[string]struct {
		lookup func(f *fixture, id uuid.UUID)
	}{
		"suspended": {func(f *fixture, id uuid.UUID) {
			f.users.EXPECT().GetByID(mock.Anything, mock.Anything, id).Return(user.User{ID: id, Status: user.StatusSuspended}, nil).Once()
		}},
		"gone": {func(f *fixture, id uuid.UUID) {
			f.users.EXPECT().GetByID(mock.Anything, mock.Anything, id).Return(user.User{}, apperr.NotFound()).Once()
		}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			id := uuid.New()
			f.expectRotate("tok", auth.RotateResult{Outcome: auth.Rotated, UserID: id, Token: auth.RefreshToken{Raw: "n", FamilyID: uuid.New()}})
			tt.lookup(f, id)

			_, err := f.svc.Refresh(context.Background(), "tok", client)

			assert.True(t, apperr.IsCode(err, apperr.CodeUnauthenticated))
		})
	}
}

func TestRefresh_StoreErrorsAreNotMaskedAsUnauthenticated(t *testing.T) {
	f := newFixture(t)
	f.expectTx()
	boom := errors.New("db down")
	f.refresh.EXPECT().Rotate(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(auth.RotateResult{}, boom).Once()

	_, err := f.svc.Refresh(context.Background(), "tok", client)

	assert.ErrorIs(t, err, boom)
}

func refreshOK(t *testing.T) (*fixture, uuid.UUID) {
	t.Helper()
	f := newFixture(t)
	u := user.User{ID: uuid.New(), Status: user.StatusActive}
	f.expectTx()
	f.refresh.EXPECT().Rotate(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(auth.RotateResult{Outcome: auth.Rotated, UserID: u.ID, Token: auth.RefreshToken{
			Raw: "rotated-token", FamilyID: uuid.New(), ExpiresAt: unitEpoch.Add(30 * 24 * time.Hour),
		}}, nil).Once()
	f.users.EXPECT().GetByID(mock.Anything, mock.Anything, u.ID).Return(u, nil).Once()
	return f, u.ID
}

func refreshRequestWith(r http.Handler, cookie, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: auth.RefreshCookieName, Value: cookie})
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestRefreshEndpoint_CookieInCookieOut(t *testing.T) {
	f, _ := refreshOK(t)

	rec := refreshRequestWith(newRouter(f), "old-cookie-token", "")

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"access_token":"`)
	assert.NotContains(t, rec.Body.String(), "rotated-token", "cookie clients never get the refresh token in the body")
	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, "rotated-token", cookies[0].Value)
	assert.True(t, cookies[0].HttpOnly)
}

func TestRefreshEndpoint_BodyInBodyOut(t *testing.T) {
	f, _ := refreshOK(t)

	rec := refreshRequestWith(newRouter(f), "", `{"refresh_token":"old-body-token"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"refresh_token":"rotated-token"`)
	assert.Empty(t, rec.Result().Cookies())
}

func TestRefreshEndpoint_FailureWithCookieClearsIt(t *testing.T) {
	f := newFixture(t)
	f.expectTx()
	f.refresh.EXPECT().Rotate(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(auth.RotateResult{Outcome: auth.Reused}, nil).Once()

	rec := refreshRequestWith(newRouter(f), "stolen", "")

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, auth.RefreshCookieName, cookies[0].Name)
	assert.Negative(t, cookies[0].MaxAge, "the dead cookie is deleted")
}

func TestRefreshEndpoint_FailureWithBodyDoesNotSetCookies(t *testing.T) {
	f := newFixture(t)
	f.expectRotate2Unknown()

	rec := refreshRequestWith(newRouter(f), "", `{"refresh_token":"nope"}`)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, rec.Result().Cookies())
}

func (f *fixture) expectRotate2Unknown() {
	f.expectTx()
	f.refresh.EXPECT().Rotate(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(auth.RotateResult{Outcome: auth.Unknown}, nil).Once()
}

func TestRefreshEndpoint_NoTokenAtAll(t *testing.T) {
	rec := refreshRequestWith(newRouter(newFixture(t)), "", "")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "VALIDATION_ERROR")
}
