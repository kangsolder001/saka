//go:build integration

package app_test

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/app"
	"github.com/kangsolder001/saka/internal/billing/payment/fake"
)

// The edge limits (spec §11.3) sit in front of the public endpoints. Each test
// spends the whole limit from one address, then checks the next request is
// refused, another address is not, and the window frees the address again.

func TestEdgeRateLimit_WebhookIsLimitedPerIPButAGatewayRetryingNormallyNeverNoticesIt(t *testing.T) {
	s := newStack(t)
	gateway := fromIP("198.51.100.20")
	body, header := fake.Webhook(paid("evt-1", "INV-2026-000001", 99000))
	post := func(from map[string]string) response {
		headers := map[string]string{}
		for k := range header {
			headers[k] = header.Get(k)
		}
		for k, v := range from {
			headers[k] = v
		}
		return s.do(http.MethodPost, webhookPath, string(body), headers)
	}

	for i := 1; i <= app.RuleWebhook.Limit; i++ {
		require.Equal(t, http.StatusOK, post(gateway).Code, "request %d is within the limit", i)
	}
	over := post(gateway)

	assert.Equal(t, http.StatusTooManyRequests, over.Code)
	assert.Equal(t, "RATE_LIMITED", errorOf(over)["code"])
	retry, err := strconv.Atoi(over.Header().Get("Retry-After"))
	require.NoError(t, err)
	assert.Positive(t, retry)
	assert.Equal(t, http.StatusOK, post(fromIP("203.0.113.77")).Code, "another address has its own counter")
	assert.Equal(t, 1, s.paymentEvents(), "the repeated deliveries were one event, and the refused ones stored nothing")

	s.clock.Advance(app.RuleWebhook.Window + time.Second)
	assert.Equal(t, http.StatusOK, post(gateway).Code, "the window passed")
}

func TestEdgeRateLimit_InvitationLinksAreLimitedPerIP(t *testing.T) {
	s := newStack(t)
	scanner := fromIP("198.51.100.30")
	lookup := func(from map[string]string) response {
		return s.do(http.MethodGet, "/api/v1/invitations/not-a-real-token", "", from)
	}

	for i := 1; i <= app.RuleInvitationLink.Limit; i++ {
		require.Equal(t, http.StatusNotFound, lookup(scanner).Code, "lookup %d is within the limit", i)
	}

	assert.Equal(t, http.StatusTooManyRequests, lookup(scanner).Code)
	assert.Equal(t, http.StatusTooManyRequests,
		s.post("/api/v1/invitations/not-a-real-token/decline", "", scanner).Code, "lookups, accepts and declines share one counter")
	assert.Equal(t, http.StatusNotFound, lookup(fromIP("203.0.113.78")).Code)
}

func TestEdgeRateLimit_TokenEndpointsAndLoginAreLimitedPerIP(t *testing.T) {
	fixed := func(body string) func(int) string { return func(int) string { return body } }
	for _, tc := range []struct {
		name  string
		path  string
		body  func(i int) string // login uses a distinct email per request
		limit int
	}{
		{"verify-email", "/api/v1/auth/verify-email", fixed(`{"token":"nope"}`), app.RuleAuthToken.Limit},
		{"reset-password", "/api/v1/auth/reset-password", fixed(`{"token":"nope","password":"another-password"}`), app.RuleAuthToken.Limit},
		{"refresh", "/api/v1/auth/refresh", fixed(`{"refresh_token":"nope"}`), app.RuleAuthToken.Limit},
		{"confirm-email-change", "/api/v1/me/email/confirm", fixed(`{"token":"nope"}`), app.RuleAuthToken.Limit},
		{"login", "/api/v1/auth/login", func(i int) string {
			// Distinct emails: the per-account rule would otherwise stop one
			// email after five failures, before the per-IP one.
			return fmt.Sprintf(`{"email":"nobody%d@example.com","password":"wrong-password"}`, i)
		}, app.RuleLoginAttempts.Limit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newStack(t)
			client := fromIP("198.51.100.40")

			for i := 1; i <= tc.limit; i++ {
				res := s.post(tc.path, tc.body(i), client)
				require.NotEqual(t, http.StatusTooManyRequests, res.Code, "request %d is within the limit", i)
			}

			assert.Equal(t, http.StatusTooManyRequests, s.post(tc.path, tc.body(0), client).Code)
			assert.NotEqual(t, http.StatusTooManyRequests, s.post(tc.path, tc.body(0), fromIP("203.0.113.79")).Code,
				"another address has its own counter")
		})
	}
}

func TestEdgeRateLimit_CreatingOrganizationsIsLimitedPerUserNotPerAddress(t *testing.T) {
	s := newStack(t)
	ann := s.loggedIn(s.registered("ann@example.com"))
	bob := s.loggedIn(s.registered("bob@example.com"))
	create := func(access string, n int, from string) response {
		headers := bearer(access)
		headers[remoteAddrHeader] = from
		return s.post(orgsPath, fmt.Sprintf(`{"name":"Team %d"}`, n), headers)
	}

	for i := 1; i <= app.RuleCreateOrganization.Limit; i++ {
		require.Equal(t, http.StatusCreated, create(ann, i, "198.51.100.50").Code, "organization %d", i)
	}

	over := create(ann, 99, "203.0.113.80")
	assert.Equal(t, http.StatusTooManyRequests, over.Code, "moving to another address does not help")
	assert.Equal(t, "RATE_LIMITED", errorOf(over)["code"])
	assert.Equal(t, http.StatusCreated, create(bob, 1, "198.51.100.50").Code, "another user on the same address is unaffected")
}

func TestEdgeRateLimit_ThePublicCatalogIsLimitedPerIP(t *testing.T) {
	s := newStack(t)
	reader := fromIP("198.51.100.60")

	for i := 1; i <= app.RulePublicCatalog.Limit; i++ {
		require.Equal(t, http.StatusOK, s.get("/api/v1/plans", reader).Code, "request %d", i)
	}

	assert.Equal(t, http.StatusTooManyRequests, s.get("/api/v1/plans", reader).Code)
	assert.Equal(t, http.StatusOK, s.get("/api/v1/plans", fromIP("203.0.113.81")).Code)
}
