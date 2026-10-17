package auth_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/auth"
	"github.com/kangsolder001/saka/internal/platform/apperr"
)

func TestGenerateToken(t *testing.T) {
	raw, hash, err := auth.GenerateToken()

	require.NoError(t, err)
	assert.Regexp(t, `^[A-Za-z0-9_-]{43}$`, raw, "256 bits, base64url without padding")
	assert.Regexp(t, `^[0-9a-f]{64}$`, hash)
	assert.Equal(t, auth.HashToken(raw), hash)
	assert.NotEqual(t, raw, hash, "the stored value must not be the raw token")
}

func TestGenerateToken_IsUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		raw, _, err := auth.GenerateToken()
		require.NoError(t, err)
		assert.False(t, seen[raw])
		seen[raw] = true
	}
}

func TestHashToken_IsDeterministicSHA256(t *testing.T) {
	// echo -n "abc" | sha256sum
	assert.Equal(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", auth.HashToken("abc"))
	assert.Equal(t, auth.HashToken("x"), auth.HashToken("x"))
	assert.NotEqual(t, auth.HashToken("x"), auth.HashToken("y"))
}

func TestTokenType_TTLMatchesSpec(t *testing.T) {
	assert.Equal(t, 24*time.Hour, auth.TokenEmailVerification.TTL())
	assert.Equal(t, time.Hour, auth.TokenPasswordReset.TTL())
	assert.Equal(t, 24*time.Hour, auth.TokenEmailChange.TTL())
}

func TestErrInvalidToken_IsAValidationError(t *testing.T) {
	err := auth.ErrInvalidToken()

	assert.True(t, apperr.IsCode(err, apperr.CodeValidation))
}
