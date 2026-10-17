package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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
	tok, _, err := s.insertWithID(ctx, q, userID, family, client)
	return tok, err
}

func (s *RefreshTokens) insertWithID(ctx context.Context, q db.DBTX, userID, family uuid.UUID, client ClientInfo) (RefreshToken, uuid.UUID, error) {
	raw, hash, err := GenerateToken()
	if err != nil {
		return RefreshToken{}, uuid.Nil, err
	}
	now := s.clk.Now()
	expires := now.Add(s.ttl)
	id, err := authdb.New(q).InsertRefreshToken(ctx, authdb.InsertRefreshTokenParams{
		UserID:    userID,
		FamilyID:  family,
		TokenHash: hash,
		UserAgent: optional(truncate(client.UserAgent, maxUserAgentLen)),
		Ip:        optional(client.IP),
		ExpiresAt: expires,
		Now:       now,
	})
	if err != nil {
		return RefreshToken{}, uuid.Nil, fmt.Errorf("auth: insert refresh token: %w", err)
	}
	return RefreshToken{Raw: raw, FamilyID: family, ExpiresAt: expires}, id, nil
}

// RotateOutcome says what Rotate did with the presented token.
type RotateOutcome int

const (
	// Rotated: the token was valid; a new one in the same family replaced it.
	Rotated RotateOutcome = iota
	// Unknown: no such token.
	Unknown
	// Expired: the token's lifetime is over.
	Expired
	// Reused: the token had already been rotated or revoked, so it was
	// presented by someone who should not have it. The whole session (family)
	// has been revoked.
	Reused
)

// RotateResult is the result of Rotate.
type RotateResult struct {
	Outcome RotateOutcome
	// Token, UserID are set when Outcome is Rotated.
	Token  RefreshToken
	UserID uuid.UUID
}

// Rotate exchanges a refresh token for a new one in the same family and
// retires the old one (replaced_by points at its successor), spec §5.3.
//
// Presenting a token that was already revoked is treated as theft: every token
// of the family is revoked. That revocation must be committed even though the
// caller then rejects the request, so detection is reported through
// RotateResult.Outcome, never as an error. q must be a transaction.
func (s *RefreshTokens) Rotate(ctx context.Context, q db.DBTX, raw string, client ClientInfo) (RotateResult, error) {
	queries := authdb.New(q)
	row, err := queries.GetRefreshTokenForUpdate(ctx, HashToken(raw))
	if errors.Is(err, pgx.ErrNoRows) {
		return RotateResult{Outcome: Unknown}, nil
	}
	if err != nil {
		return RotateResult{}, fmt.Errorf("auth: load refresh token: %w", err)
	}

	now := s.clk.Now()
	if row.RevokedAt != nil {
		if _, err := queries.RevokeSession(ctx, authdb.RevokeSessionParams{FamilyID: row.FamilyID, Now: now}); err != nil {
			return RotateResult{}, fmt.Errorf("auth: revoke compromised session: %w", err)
		}
		return RotateResult{Outcome: Reused}, nil
	}
	if !row.ExpiresAt.After(now) {
		return RotateResult{Outcome: Expired}, nil
	}

	next, nextID, err := s.insertWithID(ctx, q, row.UserID, row.FamilyID, client)
	if err != nil {
		return RotateResult{}, err
	}
	if _, err := queries.MarkRefreshTokenRotated(ctx, authdb.MarkRefreshTokenRotatedParams{
		ID: row.ID, ReplacedBy: &nextID, Now: now,
	}); err != nil {
		return RotateResult{}, fmt.Errorf("auth: retire refresh token: %w", err)
	}
	return RotateResult{Outcome: Rotated, Token: next, UserID: row.UserID}, nil
}

// RevokeSession revokes every refresh token of one login (logout).
func (s *RefreshTokens) RevokeSession(ctx context.Context, q db.DBTX, familyID uuid.UUID) error {
	if _, err := authdb.New(q).RevokeSession(ctx, authdb.RevokeSessionParams{FamilyID: familyID, Now: s.clk.Now()}); err != nil {
		return fmt.Errorf("auth: revoke session: %w", err)
	}
	return nil
}

// RevokeAllSessions revokes every refresh token of a user (password reset).
func (s *RefreshTokens) RevokeAllSessions(ctx context.Context, q db.DBTX, userID uuid.UUID) error {
	if _, err := authdb.New(q).RevokeAllSessionsOfUser(ctx, authdb.RevokeAllSessionsOfUserParams{UserID: userID, Now: s.clk.Now()}); err != nil {
		return fmt.Errorf("auth: revoke all sessions: %w", err)
	}
	return nil
}

// RevokeOtherSessions revokes every session of the user except keepFamilyID
// (password change: the caller stays logged in, every other device does not).
func (s *RefreshTokens) RevokeOtherSessions(ctx context.Context, q db.DBTX, userID, keepFamilyID uuid.UUID) error {
	if _, err := authdb.New(q).RevokeOtherSessionsOfUser(ctx, authdb.RevokeOtherSessionsOfUserParams{
		UserID: userID, KeepFamilyID: keepFamilyID, Now: s.clk.Now(),
	}); err != nil {
		return fmt.Errorf("auth: revoke other sessions: %w", err)
	}
	return nil
}

// MaxListedSessions caps how many sessions ListSessions returns (newest first).
const MaxListedSessions = 100

// SessionInfo describes one live login of a user.
type SessionInfo struct {
	// ID is the session (refresh token family) ID, also the sid claim of its
	// access tokens.
	ID         uuid.UUID
	UserAgent  string
	IP         string
	StartedAt  time.Time
	LastUsedAt time.Time
	ExpiresAt  time.Time
}

// ListSessions returns the user's live sessions, most recently used first.
func (s *RefreshTokens) ListSessions(ctx context.Context, q db.DBTX, userID uuid.UUID) ([]SessionInfo, error) {
	rows, err := authdb.New(q).ListActiveSessions(ctx, authdb.ListActiveSessionsParams{
		UserID: userID, Now: s.clk.Now(), MaxRows: MaxListedSessions,
	})
	if err != nil {
		return nil, fmt.Errorf("auth: list sessions: %w", err)
	}
	out := make([]SessionInfo, len(rows))
	for i, r := range rows {
		out[i] = SessionInfo{
			ID:         r.FamilyID,
			UserAgent:  deref(r.UserAgent),
			IP:         deref(r.Ip),
			StartedAt:  r.StartedAt,
			LastUsedAt: r.LastUsedAt,
			ExpiresAt:  r.ExpiresAt,
		}
	}
	return out, nil
}

// RevokeUserSession ends one of the user's live sessions. It reports false
// when the user has no live session with that ID (unknown, someone else's,
// already ended or expired), and the caller cannot tell those apart.
func (s *RefreshTokens) RevokeUserSession(ctx context.Context, q db.DBTX, userID, sessionID uuid.UUID) (bool, error) {
	n, err := authdb.New(q).RevokeUserSession(ctx, authdb.RevokeUserSessionParams{
		FamilyID: sessionID, UserID: userID, Now: s.clk.Now(),
	})
	if err != nil {
		return false, fmt.Errorf("auth: revoke user session: %w", err)
	}
	return n > 0, nil
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

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
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
