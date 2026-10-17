//go:build integration

package app_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (s *stack) cachedUserKeys() int {
	s.t.Helper()
	keys, err := s.redis.Raw().Keys(context.Background(), s.redis.Namespace("user").Key("summary:v1:*")).Result()
	require.NoError(s.t, err)
	return len(keys)
}

func TestUserCache_AuthenticatedRequestsFillItAndStayOutOfThePasswordHash(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	access := s.loggedIn("ann@example.com")
	require.Zero(t, s.cachedUserKeys(), "logging in alone does not cache anything")

	require.Equal(t, http.StatusOK, s.post(resendPath, ``, bearer(access)).Code)

	assert.Equal(t, 1, s.cachedUserKeys(), "the authenticated request cached the member")
	keys, err := s.redis.Raw().Keys(context.Background(), s.redis.Namespace("user").Key("summary:v1:*")).Result()
	require.NoError(t, err)
	raw, err := s.redis.Raw().Get(context.Background(), keys[0]).Result()
	require.NoError(t, err)
	assert.NotContains(t, raw, "argon2id")
	assert.NotContains(t, raw, "password")
}

func TestUserCache_SuspensionTakesEffectOnceTheCacheIsInvalidated(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	access := s.loggedIn("ann@example.com")
	require.Equal(t, http.StatusOK, s.post(resendPath, ``, bearer(access)).Code)

	_, err := s.db.Pool().Exec(context.Background(), "UPDATE users SET status = 'suspended'")
	require.NoError(t, err)

	assert.NotEqual(t, http.StatusUnauthorized, s.post(resendPath, ``, bearer(access)).Code,
		"documented trade-off: until invalidated (or the TTL passes) the cached 'active' is served")

	s.invalidateAllUsers()
	assert.Equal(t, http.StatusUnauthorized, s.post(resendPath, ``, bearer(access)).Code, "invalidation makes the suspension immediate")
}

func TestUserCache_VerifyingTheEmailIsVisibleImmediately(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	access := s.loggedIn("ann@example.com")
	require.Equal(t, http.StatusOK, s.post(resendPath, ``, bearer(access)).Code, "caches 'unverified'")
	token := s.latestEmailToken("ann@example.com")

	require.Equal(t, http.StatusOK, s.post(verifyPath, `{"token":"`+token+`"}`).Code)

	res := s.post(resendPath, ``, bearer(access))
	assert.Equal(t, http.StatusConflict, res.Code, "a stale 'unverified' entry would have sent another email (200)")
}

func TestUserCache_RefreshUsesTheCacheAndStillRejectsInvalidatedSuspensions(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	sess := s.cookieLogin("ann@example.com")
	require.Equal(t, http.StatusOK, s.post(resendPath, ``, bearer(sess.access)).Code) // caches the member
	first, next := s.refreshWithCookie(sess.refresh)
	require.Equal(t, http.StatusOK, first.Code)

	_, err := s.db.Pool().Exec(context.Background(), "UPDATE users SET status = 'suspended'")
	require.NoError(t, err)
	s.invalidateAllUsers()

	res, _ := s.refreshWithCookie(next.refresh)
	assert.Equal(t, http.StatusUnauthorized, res.Code)
}

func TestUserCache_ARedisOutageDoesNotLogAuthenticatedUsersOut(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	sess := s.cookieLogin("ann@example.com")
	require.NoError(t, s.redis.Close())

	// logout needs authentication but no rate limiting, so it isolates the user lookup.
	res := s.post(logoutPath, ``, bearer(sess.access))

	assert.Equal(t, http.StatusNoContent, res.Code, "member reads fall back to PostgreSQL when the cache is unavailable")
}

func TestRedisOutage_RateLimitedEndpointsFailClosed(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	require.NoError(t, s.redis.Close())

	res := s.post(loginPath, loginBody("ann@example.com", "correct-horse"))

	assert.Equal(t, http.StatusInternalServerError, res.Code, "never skip the brute-force limit just because Redis is down")
}
