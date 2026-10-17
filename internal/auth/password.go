// Package auth implements registration, login, tokens, email verification and
// password reset (spec §5).
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"

	"github.com/kangsolder001/saka/internal/platform/apperr"
)

// Password length limits (spec §5.2).
const (
	MinPasswordLength = 8
	MaxPasswordLength = 72
)

// Argon2Params are the Argon2id cost settings. They are stored inside every
// hash, so they can be raised later without invalidating existing passwords.
type Argon2Params struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
}

// DefaultArgon2Params follows the OWASP minimum recommendation for Argon2id
// (19 MiB, 2 iterations, 1 lane).
func DefaultArgon2Params() Argon2Params {
	return Argon2Params{MemoryKiB: 19 * 1024, Iterations: 2, Parallelism: 1}
}

const (
	saltLen = 16
	keyLen  = 32
	// Sanity limits applied to parameters read back from a stored hash.
	maxStoredMemoryKiB  = 1 << 20 // 1 GiB
	maxStoredIterations = 20
)

// PasswordHasher hashes and verifies passwords with Argon2id. It is safe for
// concurrent use.
type PasswordHasher struct {
	params    Argon2Params
	dummyHash string
}

// NewPasswordHasher creates a hasher with the given cost.
func NewPasswordHasher(params Argon2Params) (*PasswordHasher, error) {
	if params.MemoryKiB < 8*uint32(params.Parallelism) || params.Iterations < 1 || params.Parallelism < 1 {
		return nil, errors.New("invalid argon2 parameters")
	}
	h := &PasswordHasher{params: params}
	dummy, err := h.Hash("dummy-password-for-timing-equalisation")
	if err != nil {
		return nil, err
	}
	h.dummyHash = dummy
	return h, nil
}

// Hash returns the password as a PHC string:
// $argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>.
func (h *PasswordHasher) Hash(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, h.params.Iterations, h.params.MemoryKiB, h.params.Parallelism, keyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.params.MemoryKiB, h.params.Iterations, h.params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify checks password against an encoded hash in constant time. needsRehash
// is true when the hash was made with weaker parameters than the current ones
// and should be replaced after a successful login.
func (h *PasswordHasher) Verify(password, encoded string) (ok, needsRehash bool, err error) {
	params, salt, want, err := decode(encoded)
	if err != nil {
		return false, false, err
	}
	got := argon2.IDKey([]byte(password), salt, params.Iterations, params.MemoryKiB, params.Parallelism, uint32(len(want))) //nolint:gosec // len(want) is bounded by decode
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, false, nil
	}
	return true, params != h.params, nil
}

// VerifyDummy spends the same time as a real verification. Login calls it for
// unknown emails so response time does not reveal whether an account exists.
func (h *PasswordHasher) VerifyDummy(password string) {
	_, _, _ = h.Verify(password, h.dummyHash)
}

var errBadHash = errors.New("malformed password hash")

func decode(encoded string) (Argon2Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	// "", "argon2id", "v=19", "m=..,t=..,p=..", salt, hash
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return Argon2Params{}, nil, nil, errBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Argon2Params{}, nil, nil, errBadHash
	}
	var p Argon2Params
	var parallelism uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.MemoryKiB, &p.Iterations, &parallelism); err != nil || parallelism < 1 || parallelism > 255 {
		return Argon2Params{}, nil, nil, errBadHash
	}
	p.Parallelism = uint8(parallelism) //nolint:gosec // range checked above
	if p.Iterations < 1 || p.Iterations > maxStoredIterations || p.MemoryKiB < 8*uint32(p.Parallelism) || p.MemoryKiB > maxStoredMemoryKiB {
		return Argon2Params{}, nil, nil, errBadHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 {
		return Argon2Params{}, nil, nil, errBadHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) < 16 || len(key) > 128 {
		return Argon2Params{}, nil, nil, errBadHash
	}
	return p, salt, key, nil
}

// ValidatePassword enforces the password policy: 8 to 72 characters and not
// equal to the account's email. It returns a VALIDATION_ERROR on the
// "password" field.
func ValidatePassword(password, email string) error {
	n := utf8.RuneCountInString(password)
	switch {
	case n < MinPasswordLength:
		return passwordError(fmt.Sprintf("must be at least %d characters", MinPasswordLength))
	case n > MaxPasswordLength:
		return passwordError(fmt.Sprintf("must be at most %d characters", MaxPasswordLength))
	case strings.EqualFold(strings.TrimSpace(password), strings.TrimSpace(email)):
		return passwordError("must not be the same as your email")
	}
	return nil
}

func passwordError(reason string) error {
	return apperr.Validation(map[string]string{"password": reason})
}
