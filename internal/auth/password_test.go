package auth_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/auth"
	"github.com/kangsolder001/saka/internal/platform/apperr"
)

// lightParams keeps tests fast; production uses DefaultArgon2Params.
var lightParams = auth.Argon2Params{MemoryKiB: 64, Iterations: 1, Parallelism: 1}

func newHasher(t *testing.T) *auth.PasswordHasher {
	t.Helper()
	h, err := auth.NewPasswordHasher(lightParams)
	require.NoError(t, err)
	return h
}

func TestHashAndVerify(t *testing.T) {
	h := newHasher(t)

	encoded, err := h.Hash("correct horse battery")
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(encoded, "$argon2id$v=19$m=64,t=1,p=1$"), encoded)
	ok, needsRehash, err := h.Verify("correct horse battery", encoded)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.False(t, needsRehash)

	ok, _, err = h.Verify("wrong password", encoded)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestHash_SaltsEveryHash(t *testing.T) {
	h := newHasher(t)

	a, err := h.Hash("same password")
	require.NoError(t, err)
	b, err := h.Hash("same password")
	require.NoError(t, err)

	assert.NotEqual(t, a, b)
}

func TestVerify_FlagsHashesWithWeakerParameters(t *testing.T) {
	weak := newHasher(t)
	strong, err := auth.NewPasswordHasher(auth.Argon2Params{MemoryKiB: 128, Iterations: 2, Parallelism: 1})
	require.NoError(t, err)
	encoded, err := weak.Hash("password-123")
	require.NoError(t, err)

	ok, needsRehash, err := strong.Verify("password-123", encoded)

	require.NoError(t, err)
	assert.True(t, ok, "old hashes keep working when the cost is raised")
	assert.True(t, needsRehash)
}

func TestVerify_RejectsMalformedHashes(t *testing.T) {
	h := newHasher(t)
	valid, err := h.Hash("password-123")
	require.NoError(t, err)
	parts := strings.Split(valid, "$")

	tests := map[string]string{
		"empty":             "",
		"plain text":        "not-a-hash",
		"wrong algorithm":   strings.Replace(valid, "argon2id", "argon2i", 1),
		"wrong version":     strings.Replace(valid, "v=19", "v=16", 1),
		"missing parts":     "$argon2id$v=19$m=64,t=1,p=1$c2FsdHNhbHQ",
		"zero iterations":   "$argon2id$v=19$m=64,t=0,p=1$" + parts[4] + "$" + parts[5],
		"absurd memory":     "$argon2id$v=19$m=4294967295,t=1,p=1$" + parts[4] + "$" + parts[5],
		"absurd iterations": "$argon2id$v=19$m=64,t=999999,p=1$" + parts[4] + "$" + parts[5],
		"bad salt":          "$argon2id$v=19$m=64,t=1,p=1$!!!$" + parts[5],
		"short key":         "$argon2id$v=19$m=64,t=1,p=1$" + parts[4] + "$AAAA",
	}
	for name, encoded := range tests {
		t.Run(name, func(t *testing.T) {
			ok, _, err := h.Verify("password-123", encoded)

			assert.Error(t, err)
			assert.False(t, ok)
		})
	}
}

func TestNewPasswordHasher_RejectsInvalidParams(t *testing.T) {
	for _, p := range []auth.Argon2Params{
		{MemoryKiB: 4, Iterations: 1, Parallelism: 1},
		{MemoryKiB: 64, Iterations: 0, Parallelism: 1},
		{MemoryKiB: 64, Iterations: 1, Parallelism: 0},
	} {
		_, err := auth.NewPasswordHasher(p)
		assert.Error(t, err, "%+v", p)
	}
}

func TestVerifyDummy_DoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() { newHasher(t).VerifyDummy("anything") })
}

func TestDefaultArgon2Params_MeetOWASPMinimum(t *testing.T) {
	p := auth.DefaultArgon2Params()

	assert.GreaterOrEqual(t, p.MemoryKiB, uint32(19*1024))
	assert.GreaterOrEqual(t, p.Iterations, uint32(2))
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		email    string
		wantMsg  string
	}{
		{"ok", "longenough", "a@b.co", ""},
		{"exactly min", "12345678", "a@b.co", ""},
		{"exactly max", strings.Repeat("a", 72), "a@b.co", ""},
		{"too short", "short", "a@b.co", "at least 8"},
		{"empty", "", "a@b.co", "at least 8"},
		{"too long", strings.Repeat("a", 73), "a@b.co", "at most 72"},
		{"same as email", "user@example.com", "user@example.com", "same as your email"},
		{"same as email ignoring case", "USER@example.com", "user@example.com", "same as your email"},
		{"same as email with spaces", " user@example.com ", "user@example.com", "same as your email"},
		{"counts characters not bytes", strings.Repeat("é", 8), "a@b.co", ""},
		{"too long in characters", strings.Repeat("é", 73), "a@b.co", "at most 72"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := auth.ValidatePassword(tt.password, tt.email)

			if tt.wantMsg == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.True(t, apperr.IsCode(err, apperr.CodeValidation))
			fields := apperr.From(err).Details["fields"].(map[string]string)
			assert.Contains(t, fields["password"], tt.wantMsg)
		})
	}
}
