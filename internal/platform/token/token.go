// Package token generates and hashes the opaque random tokens used in email
// links and cookies (verification, password reset, invitations, refresh).
// The raw value goes to the user; only its hash is stored.
package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// Bytes is the entropy of every opaque token (256 bits).
const Bytes = 32

// Generate returns a new random token and its storage hash.
func Generate() (raw, hash string, err error) {
	b := make([]byte, Bytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, Hash(raw), nil
}

// Hash returns the hex SHA-256 of a raw token. A fast hash is appropriate
// here: tokens are 256-bit random values, not guessable passwords.
func Hash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
