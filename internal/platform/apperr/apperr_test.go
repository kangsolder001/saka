package apperr_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/apperr"
)

func TestHTTPStatusMapping(t *testing.T) {
	tests := []struct {
		err  *apperr.Error
		want int
	}{
		{apperr.Validation(nil), http.StatusBadRequest},
		{apperr.Unauthenticated(), http.StatusUnauthorized},
		{apperr.TokenExpired(), http.StatusUnauthorized},
		{apperr.Forbidden(), http.StatusForbidden},
		{apperr.EmailNotVerified(), http.StatusForbidden},
		{apperr.PlanFeatureUnavailable("Fitur tidak tersedia.", "audit_log", "free", nil), http.StatusForbidden},
		{apperr.PlanLimitReached("Batas tercapai.", "members", 3, 3, "free", nil), http.StatusPaymentRequired},
		{apperr.SubscriptionInactive(), http.StatusPaymentRequired},
		{apperr.NotFound(), http.StatusNotFound},
		{apperr.Conflict("email sudah terdaftar"), http.StatusConflict},
		{apperr.RateLimited(), http.StatusTooManyRequests},
		{apperr.PaymentProviderError(errors.New("boom")), http.StatusBadGateway},
		{apperr.Internal(errors.New("boom")), http.StatusInternalServerError},
		{apperr.New("UNKNOWN", "x"), http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(string(tt.err.Code), func(t *testing.T) {
			assert.Equal(t, tt.want, tt.err.HTTPStatus())
		})
	}
}

func TestResponse_PlanLimitReachedCarriesTheUpgradeDetails(t *testing.T) {
	pro := "pro"
	status, body := apperr.PlanLimitReached("Paket Free hanya mendukung 3 anggota.", "members", 3, 3, "free", &pro).Response()

	raw, err := json.Marshal(body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusPaymentRequired, status)
	assert.JSONEq(t, `{"error":{"code":"PLAN_LIMIT_REACHED","message":"Paket Free hanya mendukung 3 anggota.","details":{"limit":"members","max":3,"used":3,"plan":"free","upgrade_to":"pro"}}}`, string(raw))
}

func TestResponse_PlanLimitReachedWithoutAnUpgradeSaysNull(t *testing.T) {
	_, body := apperr.PlanLimitReached("Batas tercapai.", "members", 10, 10, "business", nil).Response()

	raw, err := json.Marshal(body)
	require.NoError(t, err)

	assert.Contains(t, string(raw), `"upgrade_to":null`)
}

func TestResponse_PlanFeatureUnavailableCarriesTheUpgradeDetails(t *testing.T) {
	business := "business"
	status, body := apperr.PlanFeatureUnavailable("Fitur ini tidak tersedia di paket Pro.", "audit_log", "pro", &business).Response()

	raw, err := json.Marshal(body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusForbidden, status)
	assert.JSONEq(t, `{"error":{"code":"PLAN_FEATURE_UNAVAILABLE","message":"Fitur ini tidak tersedia di paket Pro.","details":{"feature":"audit_log","plan":"pro","upgrade_to":"business"}}}`, string(raw))
}

func TestResponse_DetailsIsAlwaysAnObject(t *testing.T) {
	_, body := apperr.NotFound().Response()

	raw, err := json.Marshal(body)
	require.NoError(t, err)

	assert.Contains(t, string(raw), `"details":{}`)
}

func TestResponse_ValidationFields(t *testing.T) {
	_, body := apperr.Validation(map[string]string{"email": "invalid format"}).Response()

	assert.Equal(t, map[string]any{"fields": map[string]string{"email": "invalid format"}}, body.Error.Details)
}

func TestResponse_InternalNeverLeaksCause(t *testing.T) {
	err := apperr.Internal(errors.New("pq: password authentication failed")).
		WithDetails(map[string]any{"secret": "x"})

	status, body := err.Response()
	raw, _ := json.Marshal(body)

	assert.Equal(t, http.StatusInternalServerError, status)
	assert.NotContains(t, string(raw), "password")
	assert.NotContains(t, string(raw), "secret")
}

func TestFrom(t *testing.T) {
	t.Run("keeps application errors, even when wrapped", func(t *testing.T) {
		wrapped := fmt.Errorf("create user: %w", apperr.Conflict("email sudah terdaftar"))

		got := apperr.From(wrapped)

		assert.Equal(t, apperr.CodeConflict, got.Code)
	})

	t.Run("turns unknown errors into internal errors", func(t *testing.T) {
		cause := errors.New("disk full")

		got := apperr.From(cause)

		assert.Equal(t, apperr.CodeInternal, got.Code)
		assert.ErrorIs(t, got, cause)
	})
}

func TestIsCodeAndIs(t *testing.T) {
	err := fmt.Errorf("lookup: %w", apperr.NotFound())

	assert.True(t, apperr.IsCode(err, apperr.CodeNotFound))
	assert.False(t, apperr.IsCode(err, apperr.CodeConflict))
	assert.ErrorIs(t, err, apperr.NotFound())
	assert.False(t, apperr.IsCode(errors.New("plain"), apperr.CodeNotFound))
}

func TestWithDetailsDoesNotMutateOriginal(t *testing.T) {
	base := apperr.Forbidden()

	_ = base.WithDetails(map[string]any{"a": 1})

	assert.Nil(t, base.Details)
}

func TestRateLimitedAfter(t *testing.T) {
	err := apperr.RateLimitedAfter(90*time.Second + 200*time.Millisecond)

	assert.Equal(t, apperr.CodeRateLimited, err.Code)
	assert.Equal(t, http.StatusTooManyRequests, err.HTTPStatus())
	assert.Equal(t, 91, err.Details[apperr.DetailRetryAfterSeconds], "rounded up so clients never retry too early")
}
