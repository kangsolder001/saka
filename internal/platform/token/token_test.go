package token_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/token"
)

func TestGenerate_ReturnsAnUnguessableTokenAndItsHash(t *testing.T) {
	raw, hash, err := token.Generate()

	require.NoError(t, err)
	assert.Len(t, raw, 43, "32 bytes in unpadded base64url")
	assert.Equal(t, token.Hash(raw), hash)
	assert.Len(t, hash, 64, "hex SHA-256")
	assert.NotContains(t, hash, raw)
}

func TestGenerate_NeverRepeats(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		raw, _, err := token.Generate()
		require.NoError(t, err)
		assert.False(t, seen[raw])
		seen[raw] = true
	}
}

func TestHash_IsStableAndInputSensitive(t *testing.T) {
	assert.Equal(t, token.Hash("abc"), token.Hash("abc"))
	assert.NotEqual(t, token.Hash("abc"), token.Hash("abd"))
	assert.Equal(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", token.Hash("abc"))
}
