package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/auth/authdb"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/db"
)

// tokenBytes is the entropy of every opaque token (256 bits).
const tokenBytes = 32

// GenerateToken returns a new random token and its storage hash. The raw
// value goes to the user (email link, cookie); only the hash is stored.
func GenerateToken() (raw, hash string, err error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, HashToken(raw), nil
}

// HashToken returns the hex SHA-256 of a raw token. A fast hash is
// appropriate here: tokens are 256-bit random values, not guessable passwords.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// TokenType is the purpose of a single-use token.
type TokenType string

//nolint:gosec // G101 false positives: these are purpose names, not credentials
const (
	TokenEmailVerification TokenType = "email_verification"
	TokenPasswordReset     TokenType = "password_reset"
	TokenEmailChange       TokenType = "email_change"
)

// TTL is how long a token of this type stays valid (spec §4.2).
func (t TokenType) TTL() time.Duration {
	switch t {
	case TokenPasswordReset:
		return time.Hour
	default: // email verification and email change
		return 24 * time.Hour
	}
}

// UserToken is a consumed single-use token.
type UserToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Type      TokenType
	Payload   json.RawMessage
	ExpiresAt time.Time
}

// ErrInvalidToken is returned when a token is unknown, expired, already used
// or of the wrong type. The cases are deliberately indistinguishable.
func ErrInvalidToken() error {
	return apperr.Validation(map[string]string{"token": "invalid or expired"}) //nolint:gosec // G101 false positive: a field name, not a credential
}

// UserTokens issues and consumes single-use tokens (verification, reset, ...).
type UserTokens struct {
	clk clock.Clock
}

// NewUserTokens creates the service.
func NewUserTokens(clk clock.Clock) *UserTokens {
	return &UserTokens{clk: clk}
}

// Issue creates a token for the user and returns its raw value, which the
// caller must deliver (by email) and never store or log. Unused tokens of the
// same type for this user are removed, so only the newest link works.
// payload (optional) is stored as JSON, e.g. {"new_email": "..."}.
func (s *UserTokens) Issue(ctx context.Context, q db.DBTX, userID uuid.UUID, typ TokenType, payload any) (string, error) {
	var payloadJSON []byte
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return "", fmt.Errorf("auth: marshal token payload: %w", err)
		}
		payloadJSON = b
	}

	raw, hash, err := GenerateToken()
	if err != nil {
		return "", err
	}

	queries := authdb.New(q)
	if err := queries.DeleteOpenUserTokens(ctx, authdb.DeleteOpenUserTokensParams{UserID: userID, Type: string(typ)}); err != nil {
		return "", fmt.Errorf("auth: remove open tokens: %w", err)
	}
	now := s.clk.Now()
	if _, err := queries.InsertUserToken(ctx, authdb.InsertUserTokenParams{
		UserID:    userID,
		Type:      string(typ),
		TokenHash: hash,
		Payload:   payloadJSON,
		ExpiresAt: now.Add(typ.TTL()),
		Now:       now,
	}); err != nil {
		return "", fmt.Errorf("auth: insert token: %w", err)
	}
	return raw, nil
}

// Consume validates and uses a token in one atomic step. It fails with
// ErrInvalidToken when the token is unknown, expired, already used, or issued
// for another purpose. Of concurrent calls with the same token exactly one
// succeeds.
func (s *UserTokens) Consume(ctx context.Context, q db.DBTX, raw string, typ TokenType) (UserToken, error) {
	row, err := authdb.New(q).ConsumeUserToken(ctx, authdb.ConsumeUserTokenParams{
		TokenHash: HashToken(raw),
		Type:      string(typ),
		Now:       s.clk.Now(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return UserToken{}, ErrInvalidToken()
	}
	if err != nil {
		return UserToken{}, fmt.Errorf("auth: consume token: %w", err)
	}
	return UserToken{
		ID:        row.ID,
		UserID:    row.UserID,
		Type:      TokenType(row.Type),
		Payload:   row.Payload,
		ExpiresAt: row.ExpiresAt,
	}, nil
}
