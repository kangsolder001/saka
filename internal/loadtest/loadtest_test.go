package loadtest_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/billing/payment/fake"
	"github.com/kangsolder001/saka/internal/loadtest"
)

// stubAPI answers like the real API would for a healthy, fresh database and
// records what it was sent.
type stubAPI struct {
	mu         sync.Mutex
	paths      map[string]int
	forwarded  map[string]bool
	registered map[string]bool
	badSigs    int
}

func newStubAPI(t *testing.T) (*stubAPI, *httptest.Server) {
	t.Helper()
	api := &stubAPI{paths: map[string]int{}, forwarded: map[string]bool{}, registered: map[string]bool{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		api.mu.Lock()
		defer api.mu.Unlock()
		api.paths[r.Method+" "+r.URL.Path]++
		if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
			api.forwarded[ip] = true
		}
		switch r.URL.Path {
		case "/api/v1/plans":
			w.WriteHeader(http.StatusOK)
		case "/api/v1/auth/register":
			var in map[string]string
			_ = json.Unmarshal(body, &in)
			api.registered[in["email"]] = true
			w.WriteHeader(http.StatusCreated)
		case "/api/v1/auth/login":
			var in map[string]string
			_ = json.Unmarshal(body, &in)
			if !api.registered[in["email"]] {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
		case "/api/v1/webhooks/fake":
			if r.Header.Get(fake.SignatureHeader) != fake.Sign(body) {
				api.badSigs++
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return api, srv
}

func TestRun_EveryScenarioSendsTheRightRequestsAndAllSucceed(t *testing.T) {
	api, srv := newStubAPI(t)

	reports, err := loadtest.Run(context.Background(), loadtest.Config{
		BaseURL: srv.URL, Scenario: loadtest.ScenarioAll, Concurrency: 4, Requests: 40, Users: 5, SpoofIP: true,
	})

	require.NoError(t, err)
	require.Len(t, reports, 4)
	for _, r := range reports {
		assert.Equal(t, 40, r.Requests, r.Scenario)
		assert.Zero(t, r.Failed, r.Scenario)
		assert.Positive(t, r.RPS, r.Scenario)
		assert.LessOrEqual(t, r.P50, r.P95, r.Scenario)
		assert.LessOrEqual(t, r.P95, r.P99, r.Scenario)
		assert.LessOrEqual(t, r.P99, r.Max, r.Scenario)
	}
	assert.Equal(t, 40, api.paths["GET /api/v1/plans"])
	assert.Equal(t, 40+5, api.paths["POST /api/v1/auth/register"], "the register scenario plus the accounts the login scenario creates")
	assert.Equal(t, 40, api.paths["POST /api/v1/auth/login"])
	assert.Equal(t, 40, api.paths["POST /api/v1/webhooks/fake"])
	assert.Zero(t, api.badSigs, "webhooks are signed like the fake gateway signs them")
	assert.Greater(t, len(api.forwarded), 100, "every request has its own client address")
}

func TestRun_WithoutSpoofingNoForwardedHeaderIsSent(t *testing.T) {
	api, srv := newStubAPI(t)

	_, err := loadtest.Run(context.Background(), loadtest.Config{
		BaseURL: srv.URL, Scenario: loadtest.ScenarioPlans, Concurrency: 2, Requests: 6,
	})

	require.NoError(t, err)
	assert.Empty(t, api.forwarded)
}

func TestRun_UnexpectedStatusesAreCountedAsFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	reports, err := loadtest.Run(context.Background(), loadtest.Config{
		BaseURL: srv.URL, Scenario: loadtest.ScenarioPlans, Concurrency: 2, Requests: 10,
	})

	require.NoError(t, err)
	assert.Equal(t, 10, reports[0].Failed)
	assert.Equal(t, map[int]int{http.StatusTooManyRequests: 10}, reports[0].Statuses)
	assert.Contains(t, reports[0].String(), "429 x10")
}

func TestRun_AnUnreachableAPIIsCountedNotFatalForMeasuredScenarios(t *testing.T) {
	_, srv := newStubAPI(t)
	url := srv.URL
	srv.Close()

	reports, err := loadtest.Run(context.Background(), loadtest.Config{
		BaseURL: url, Scenario: loadtest.ScenarioPlans, Concurrency: 2, Requests: 4,
		HTTPClient: &http.Client{Timeout: time.Second},
	})

	require.NoError(t, err)
	assert.Equal(t, 4, reports[0].Failed)
	assert.Empty(t, reports[0].Statuses)
}

func TestRun_LoginSetupFailureIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	_, err := loadtest.Run(context.Background(), loadtest.Config{
		BaseURL: srv.URL, Scenario: loadtest.ScenarioLogin, Concurrency: 2, Requests: 4,
	})

	require.ErrorContains(t, err, "answered 429")
}

func TestRun_RejectsBadConfiguration(t *testing.T) {
	for name, cfg := range map[string]loadtest.Config{
		"no base url":      {Scenario: loadtest.ScenarioPlans, Concurrency: 1, Requests: 1},
		"unknown scenario": {BaseURL: "http://x", Scenario: "nope", Concurrency: 1, Requests: 1},
		"no concurrency":   {BaseURL: "http://x", Scenario: loadtest.ScenarioPlans, Requests: 1},
		"no requests":      {BaseURL: "http://x", Scenario: loadtest.ScenarioPlans, Concurrency: 1},
	} {
		_, err := loadtest.Run(context.Background(), cfg)
		assert.Error(t, err, name)
	}
}

func TestRun_StopsWhenTheContextIsCancelled(t *testing.T) {
	_, srv := newStubAPI(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := loadtest.Run(ctx, loadtest.Config{BaseURL: srv.URL, Scenario: loadtest.ScenarioPlans, Concurrency: 2, Requests: 1000})

	require.Error(t, err)
}
