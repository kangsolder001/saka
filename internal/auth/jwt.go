package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/clock"
)

const minSecretLen = 32

// Claims are the verified contents of an access token (spec §5.5). Role and
// organization are deliberately absent: they are read from the database on
// every request so changes apply immediately.
type Claims struct {
	UserID    uuid.UUID
	SessionID uuid.UUID // the refresh token family of this login
	IssuedAt  time.Time
	ExpiresAt time.Time
}

type jwtClaims struct {
	SessionID string `json:"sid"`
	jwt.RegisteredClaims
}

// JWT signs and verifies access tokens with HS256.
type JWT struct {
	secret []byte
	ttl    time.Duration
	clk    clock.Clock
}

// NewJWT creates the signer. The secret must be at least 32 bytes.
func NewJWT(secret string, ttl time.Duration, clk clock.Clock) (*JWT, error) {
	if len(secret) < minSecretLen {
		return nil, fmt.Errorf("jwt secret must be at least %d bytes", minSecretLen)
	}
	if ttl <= 0 {
		return nil, errors.New("jwt ttl must be positive")
	}
	return &JWT{secret: []byte(secret), ttl: ttl, clk: clk}, nil
}

// TTL is the lifetime of the tokens this signer issues.
func (j *JWT) TTL() time.Duration { return j.ttl }

// Sign issues an access token for the user's session and returns it with its
// expiry time.
func (j *JWT) Sign(userID, sessionID uuid.UUID) (token string, expiresAt time.Time, err error) {
	now := j.clk.Now()
	expiresAt = now.Add(j.ttl)
	claims := jwtClaims{
		SessionID: sessionID.String(),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(j.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return signed, expiresAt, nil
}

// Verify checks signature, algorithm and expiry. It returns apperr
// TOKEN_EXPIRED for an expired token and UNAUTHENTICATED for anything else
// that is wrong, without revealing why.
func (j *JWT) Verify(token string) (Claims, error) {
	parsed := jwtClaims{}
	_, err := jwt.ParseWithClaims(token, &parsed, func(*jwt.Token) (any, error) { return j.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), // blocks alg=none and algorithm confusion
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(j.clk.Now),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return Claims{}, apperr.TokenExpired()
		}
		return Claims{}, apperr.Unauthenticated()
	}

	userID, err := uuid.Parse(parsed.Subject)
	if err != nil {
		return Claims{}, apperr.Unauthenticated()
	}
	sessionID, err := uuid.Parse(parsed.SessionID)
	if err != nil {
		return Claims{}, apperr.Unauthenticated()
	}
	return Claims{
		UserID:    userID,
		SessionID: sessionID,
		IssuedAt:  parsed.IssuedAt.Time,
		ExpiresAt: parsed.ExpiresAt.Time,
	}, nil
}
