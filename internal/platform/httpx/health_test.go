package httpx_test

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/httpx"
	"github.com/kangsolder001/saka/internal/platform/httpx/mocks"
	"github.com/kangsolder001/saka/internal/platform/logger"
)

func newRouter(t *testing.T, pinger httpx.Pinger) http.Handler {
	t.Helper()
	cfg, err := config.LoadFrom(map[string]string{"DATABASE_URL": "postgres://u:p@localhost/db"})
	require.NoError(t, err)

	r, err := httpx.NewRouter(cfg, logger.Nop(), clock.NewFake(time.Now()), pinger)
	require.NoError(t, err)
	return r
}

func TestHealthz_AlwaysOKWithoutTouchingDependencies(t *testing.T) {
	pinger := mocks.NewMockPinger(t) // no expectations: any call fails the test
	r := newRouter(t, pinger)

	rec := do(r, http.MethodGet, "/healthz", nil)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}

func TestReadyz_OKWhenDatabaseReachable(t *testing.T) {
	pinger := mocks.NewMockPinger(t)
	pinger.EXPECT().Ping(mock.Anything).Return(nil).Once()
	r := newRouter(t, pinger)

	rec := do(r, http.MethodGet, "/readyz", nil)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"ok","checks":{"database":"ok"}}`, rec.Body.String())
}

func TestReadyz_UnavailableWhenDatabaseDownWithoutLeakingCause(t *testing.T) {
	pinger := mocks.NewMockPinger(t)
	pinger.EXPECT().Ping(mock.Anything).Return(errors.New("dial tcp 10.0.0.5:5432: refused")).Once()
	r := newRouter(t, pinger)

	rec := do(r, http.MethodGet, "/readyz", nil)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.JSONEq(t, `{"status":"unavailable","checks":{"database":"down"}}`, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "10.0.0.5")
}

func TestReadyz_PingGetsDeadline(t *testing.T) {
	pinger := mocks.NewMockPinger(t)
	pinger.EXPECT().Ping(mock.MatchedBy(func(ctx interface{ Deadline() (time.Time, bool) }) bool {
		_, ok := ctx.Deadline()
		return ok
	})).Return(nil).Once()
	r := newRouter(t, pinger)

	rec := do(r, http.MethodGet, "/readyz", nil)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRouter_UnknownRouteUsesStandardNotFound(t *testing.T) {
	r := newRouter(t, mocks.NewMockPinger(t))

	rec := do(r, http.MethodGet, "/api/v1/unknown", nil)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"NOT_FOUND"`)
	assert.NotEmpty(t, rec.Header().Get(httpx.HeaderTraceID))
}

func TestNewRouter_RejectsInvalidTrustedProxies(t *testing.T) {
	cfg, err := config.LoadFrom(map[string]string{
		"DATABASE_URL":    "postgres://u:p@localhost/db",
		"TRUSTED_PROXIES": "not-an-ip",
	})
	require.NoError(t, err)

	_, err = httpx.NewRouter(cfg, logger.Nop(), clock.NewFake(time.Now()), mocks.NewMockPinger(t))

	assert.Error(t, err)
}
