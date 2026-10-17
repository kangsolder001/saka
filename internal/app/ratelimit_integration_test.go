//go:build integration

package app_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fromIP(ip string) map[string]string { return map[string]string{remoteAddrHeader: ip} }

func TestLoginRateLimit_BlocksAfterFiveFailuresEvenForTheCorrectPassword(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	attacker := fromIP("198.51.100.1")

	for i := 1; i <= 5; i++ {
		res := s.post(loginPath, loginBody("ann@example.com", "wrong-password"), attacker)
		require.Equal(t, http.StatusUnauthorized, res.Code, "failure %d is still a normal 401", i)
	}
	blocked := s.post(loginPath, loginBody("ann@example.com", "correct-horse"), attacker)

	assert.Equal(t, http.StatusTooManyRequests, blocked.Code)
	assert.Contains(t, blocked.Body.String(), `"code":"RATE_LIMITED"`)
	retry, err := strconv.Atoi(blocked.Header().Get("Retry-After"))
	require.NoError(t, err)
	assert.Equal(t, 15*60, retry, "all failures happened now, so a full 15 minute window remains")
}

func TestLoginRateLimit_LockoutEndsWhenTheWindowPasses(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	ip := fromIP("198.51.100.1")
	for range 5 {
		s.post(loginPath, loginBody("ann@example.com", "wrong-password"), ip)
	}
	require.Equal(t, http.StatusTooManyRequests, s.post(loginPath, loginBody("ann@example.com", "correct-horse"), ip).Code)

	s.clock.Advance(15*time.Minute + time.Second)

	assert.Equal(t, http.StatusOK, s.post(loginPath, loginBody("ann@example.com", "correct-horse"), ip).Code)
}

func TestLoginRateLimit_IsPerEmailAndIPSoVictimsCannotBeLockedOutFromElsewhere(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	s.registered("bob@example.com")
	attacker := fromIP("198.51.100.1")
	for range 5 {
		s.post(loginPath, loginBody("ann@example.com", "wrong-password"), attacker)
	}

	assert.Equal(t, http.StatusTooManyRequests, s.post(loginPath, loginBody("ann@example.com", "correct-horse"), attacker).Code)
	assert.Equal(t, http.StatusOK, s.post(loginPath, loginBody("ann@example.com", "correct-horse"), fromIP("203.0.113.50")).Code,
		"the real Ann, on her own address, is not locked out")
	assert.Equal(t, http.StatusOK, s.post(loginPath, loginBody("bob@example.com", "correct-horse"), attacker).Code,
		"the same address can still log in to a different account")
}

func TestLoginRateLimit_SuccessfulLoginResetsTheCount(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	ip := fromIP("198.51.100.1")
	for range 4 {
		s.post(loginPath, loginBody("ann@example.com", "wrong-password"), ip)
	}
	require.Equal(t, http.StatusOK, s.post(loginPath, loginBody("ann@example.com", "correct-horse"), ip).Code)

	for i := 1; i <= 4; i++ {
		res := s.post(loginPath, loginBody("ann@example.com", "wrong-password"), ip)
		require.Equal(t, http.StatusUnauthorized, res.Code, "after a success the count starts again (failure %d)", i)
	}
}

func TestLoginRateLimit_UnknownEmailsAreLimitedExactlyLikeKnownOnes(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	ip := fromIP("198.51.100.1")
	statuses := func(email string) []int {
		var out []int
		for range 6 {
			out = append(out, s.post(loginPath, loginBody(email, "wrong-password"), ip).Code)
		}
		return out
	}

	known := statuses("ann@example.com")
	unknown := statuses("ghost@example.com")

	assert.Equal(t, []int{401, 401, 401, 401, 401, 429}, known)
	assert.Equal(t, known, unknown, "the sequence of responses must not reveal whether the account exists")
}

func TestForgotPasswordRateLimit_ThreePerHourPerEmailRegisteredOrNot(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")

	for _, email := range []string{"ann@example.com", "ghost@example.com"} {
		var got []int
		for range 4 {
			got = append(got, s.post(forgotPath, `{"email":"`+email+`"}`).Code)
		}
		assert.Equal(t, []int{200, 200, 200, 429}, got, email)
	}

	s.clock.Advance(time.Hour + time.Second)
	assert.Equal(t, http.StatusOK, s.post(forgotPath, `{"email":"ann@example.com"}`).Code, "allowed again after an hour")
}

func TestForgotPasswordRateLimit_IgnoresEmailCase(t *testing.T) {
	s := newStack(t)
	for _, email := range []string{"ann@example.com", "ANN@example.com", "Ann@Example.com"} {
		require.Equal(t, http.StatusOK, s.post(forgotPath, `{"email":"`+email+`"}`).Code)
	}

	assert.Equal(t, http.StatusTooManyRequests, s.post(forgotPath, `{"email":"aNN@EXAMPLE.com"}`).Code,
		"changing the case must not buy extra attempts")
}

func TestResendVerificationRateLimit_ThreePerHourPerUser(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	s.registered("bob@example.com")
	ann, bob := s.loggedIn("ann@example.com"), s.loggedIn("bob@example.com")

	var got []int
	for range 4 {
		got = append(got, s.post(resendPath, ``, bearer(ann)).Code)
	}

	assert.Equal(t, []int{200, 200, 200, 429}, got)
	assert.Equal(t, http.StatusOK, s.post(resendPath, ``, bearer(bob)).Code, "limits are per user")
}

func TestRegisterRateLimit_TenPerHourPerIP(t *testing.T) {
	s := newStack(t)
	ip := fromIP("198.51.100.1")

	for i := 1; i <= 10; i++ {
		res := s.post(registerPath, registerBody("user"+strconv.Itoa(i)+"@example.com"), ip)
		require.Equal(t, http.StatusCreated, res.Code, "sign-up %d", i)
	}
	blocked := s.post(registerPath, registerBody("user11@example.com"), ip)

	assert.Equal(t, http.StatusTooManyRequests, blocked.Code)
	assert.NotEmpty(t, blocked.Header().Get("Retry-After"))
	assert.Equal(t, http.StatusCreated, s.post(registerPath, registerBody("other@example.com"), fromIP("203.0.113.77")).Code,
		"another address is not affected")
	var users int
	require.NoError(t, s.queryRow("SELECT count(*) FROM users").Scan(&users))
	assert.Equal(t, 11, users, "the blocked request created nothing")
}
