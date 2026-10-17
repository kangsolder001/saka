package auth_test

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/auth"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/clock"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func newJWT(t *testing.T, clk clock.Clock) *auth.JWT {
	t.Helper()
	j, err := auth.NewJWT(testSecret, 15*time.Minute, clk)
	require.NoError(t, err)
	return j
}

func TestJWT_SignAndVerifyRoundTrip(t *testing.T) {
	clk := clock.NewFake(unitEpoch)
	j := newJWT(t, clk)
	userID, sessionID := uuid.New(), uuid.New()

	token, expiresAt, err := j.Sign(userID, sessionID)
	require.NoError(t, err)
	claims, err := j.Verify(token)

	require.NoError(t, err)
	assert.Equal(t, userID, claims.UserID)
	assert.Equal(t, sessionID, claims.SessionID)
	assert.True(t, unitEpoch.Add(15*time.Minute).Equal(expiresAt))
	assert.True(t, expiresAt.Equal(claims.ExpiresAt))
	assert.True(t, unitEpoch.Equal(claims.IssuedAt))
}

func payloadOf(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	return m
}

func TestJWT_ClaimsAreExactlySubSidIatExp(t *testing.T) {
	j := newJWT(t, clock.NewFake(unitEpoch))
	userID, sessionID := uuid.New(), uuid.New()

	token, _, err := j.Sign(userID, sessionID)
	require.NoError(t, err)

	payload := payloadOf(t, token)
	assert.Len(t, payload, 4, "no role, no organization: those are read from the database (spec §5.5)")
	assert.Equal(t, userID.String(), payload["sub"])
	assert.Equal(t, sessionID.String(), payload["sid"])
	assert.EqualValues(t, unitEpoch.Unix(), payload["iat"])
	assert.EqualValues(t, unitEpoch.Add(15*time.Minute).Unix(), payload["exp"])
}

func TestJWT_ExpiryFollowsTheClock(t *testing.T) {
	clk := clock.NewFake(unitEpoch)
	j := newJWT(t, clk)
	token, _, err := j.Sign(uuid.New(), uuid.New())
	require.NoError(t, err)

	clk.Advance(15*time.Minute - time.Second)
	_, err = j.Verify(token)
	assert.NoError(t, err, "valid until the last second")

	clk.Advance(2 * time.Second)
	_, err = j.Verify(token)
	assert.True(t, apperr.IsCode(err, apperr.CodeTokenExpired), "expired tokens get TOKEN_EXPIRED so clients know to refresh")
}

func TestJWT_RejectsInvalidTokens(t *testing.T) {
	clk := clock.NewFake(unitEpoch)
	j := newJWT(t, clk)
	userID, sessionID := uuid.New(), uuid.New()
	valid, _, err := j.Sign(userID, sessionID)
	require.NoError(t, err)
	parts := strings.Split(valid, ".")

	sign := func(method jwt.SigningMethod, key any, claims jwt.MapClaims) string {
		s, err := jwt.NewWithClaims(method, claims).SignedString(key)
		require.NoError(t, err)
		return s
	}
	exp := unitEpoch.Add(time.Hour).Unix()
	good := jwt.MapClaims{"sub": userID.String(), "sid": sessionID.String(), "exp": exp}
	without := func(key string) jwt.MapClaims {
		c := jwt.MapClaims{}
		for k, v := range good {
			if k != key {
				c[k] = v
			}
		}
		return c
	}

	tests := map[string]string{
		"empty":                "",
		"garbage":              "not.a.jwt",
		"tampered payload":     parts[0] + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"`+uuid.NewString()+`","sid":"`+sessionID.String()+`","exp":`+"9999999999"+`}`)) + "." + parts[2],
		"tampered signature":   parts[0] + "." + parts[1] + "." + strings.Repeat("A", len(parts[2])),
		"wrong secret":         sign(jwt.SigningMethodHS256, []byte("another-secret-another-secret-12345"), good),
		"alg none":             sign(jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, good),
		"other HMAC algorithm": sign(jwt.SigningMethodHS512, []byte(testSecret), good),
		"missing exp":          sign(jwt.SigningMethodHS256, []byte(testSecret), without("exp")),
		"missing sub":          sign(jwt.SigningMethodHS256, []byte(testSecret), without("sub")),
		"missing sid":          sign(jwt.SigningMethodHS256, []byte(testSecret), without("sid")),
		"sub not a uuid":       sign(jwt.SigningMethodHS256, []byte(testSecret), jwt.MapClaims{"sub": "42", "sid": sessionID.String(), "exp": exp}),
		"sid not a uuid":       sign(jwt.SigningMethodHS256, []byte(testSecret), jwt.MapClaims{"sub": userID.String(), "sid": "x", "exp": exp}),
	}
	for name, token := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := j.Verify(token)

			require.Error(t, err)
			assert.True(t, apperr.IsCode(err, apperr.CodeUnauthenticated), "got %v", err)
		})
	}
}

func TestJWT_TokenFromAnotherSecretIsRejected(t *testing.T) {
	clk := clock.NewFake(unitEpoch)
	other, err := auth.NewJWT("another-secret-another-secret-12345", time.Minute, clk)
	require.NoError(t, err)
	token, _, err := other.Sign(uuid.New(), uuid.New())
	require.NoError(t, err)

	_, err = newJWT(t, clk).Verify(token)

	assert.True(t, apperr.IsCode(err, apperr.CodeUnauthenticated))
}

func TestNewJWT_Validation(t *testing.T) {
	clk := clock.NewFake(unitEpoch)

	_, err := auth.NewJWT("too-short", time.Minute, clk)
	assert.Error(t, err)
	_, err = auth.NewJWT(testSecret, 0, clk)
	assert.Error(t, err)
}
