package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/kangsolder001/saka/internal/auth/authdb"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/db"
)

// ClientInfo describes the client a refresh token was issued to.
type ClientInfo struct {
	UserAgent string
	IP        string
}

const maxUserAgentLen = 512

// RefreshToken is a freshly issued refresh token.
type RefreshToken struct {
	// Raw is the secret value for the client. It is never stored or logged.
	Raw       string
	FamilyID  uuid.UUID
	ExpiresAt time.Time
}

// RefreshTokens stores opaque refresh tokens (spec §4.3, §5.3).
type RefreshTokens struct {
	clk clock.Clock
	ttl time.Duration
}

// NewRefreshTokens creates the store. ttl is the lifetime of each token.
func NewRefreshTokens(clk clock.Clock, ttl time.Duration) *RefreshTokens {
	return &RefreshTokens{clk: clk, ttl: ttl}
}

// StartSession issues the first refresh token of a new login (a new family).
func (s *RefreshTokens) StartSession(ctx context.Context, q db.DBTX, userID uuid.UUID, client ClientInfo) (RefreshToken, error) {
	family, err := uuid.NewV7()
	if err != nil {
		return RefreshToken{}, fmt.Errorf("auth: new session id: %w", err)
	}
	return s.insert(ctx, q, userID, family, client)
}

func (s *RefreshTokens) insert(ctx context.Context, q db.DBTX, userID, family uuid.UUID, client ClientInfo) (RefreshToken, error) {
	raw, hash, err := GenerateToken()
	if err != nil {
		return RefreshToken{}, err
	}
	now := s.clk.Now()
	expires := now.Add(s.ttl)
	if _, err := authdb.New(q).InsertRefreshToken(ctx, authdb.InsertRefreshTokenParams{
		UserID:    userID,
		FamilyID:  family,
		TokenHash: hash,
		UserAgent: optional(truncate(client.UserAgent, maxUserAgentLen)),
		Ip:        optional(client.IP),
		ExpiresAt: expires,
		Now:       now,
	}); err != nil {
		return RefreshToken{}, fmt.Errorf("auth: insert refresh token: %w", err)
	}
	return RefreshToken{Raw: raw, FamilyID: family, ExpiresAt: expires}, nil
}

// SessionActive reports whether the session still has a usable refresh token.
func (s *RefreshTokens) SessionActive(ctx context.Context, q db.DBTX, userID, familyID uuid.UUID) (bool, error) {
	active, err := authdb.New(q).SessionActive(ctx, authdb.SessionActiveParams{
		FamilyID: familyID, UserID: userID, Now: s.clk.Now(),
	})
	if err != nil {
		return false, fmt.Errorf("auth: check session: %w", err)
	}
	return active, nil
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
