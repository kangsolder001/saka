// Package reqctx stores request-scoped identifiers (trace, request, user,
// organization) in a context.Context so they travel through every layer
// without being passed as separate arguments (spec §11.1).
package reqctx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/google/uuid"
)

type key int

const (
	traceIDKey key = iota
	requestIDKey
	userIDKey
	orgIDKey
	sessionIDKey
)

// WithTraceID returns a context carrying the trace ID.
func WithTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceIDKey, id)
}

// TraceID returns the trace ID, or "" when none is set.
func TraceID(ctx context.Context) string {
	id, _ := ctx.Value(traceIDKey).(string)
	return id
}

// WithRequestID returns a context carrying the request ID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestID returns the request ID, or "" when none is set.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// WithUserID returns a context carrying the authenticated user's ID.
func WithUserID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, userIDKey, id)
}

// UserID returns the authenticated user's ID and whether it is set.
func UserID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(userIDKey).(uuid.UUID)
	return id, ok
}

// WithSessionID returns a context carrying the current login session (the
// refresh token family from the access token's sid claim).
func WithSessionID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, sessionIDKey, id)
}

// SessionID returns the current session ID and whether it is set.
func SessionID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(sessionIDKey).(uuid.UUID)
	return id, ok
}

// WithOrgID returns a context carrying the current organization's ID.
// Only the tenant middleware should call this, never user input.
func WithOrgID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, orgIDKey, id)
}

// OrgID returns the current organization's ID and whether it is set.
func OrgID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(orgIDKey).(uuid.UUID)
	return id, ok
}

// NewTraceID generates a 32-character lowercase hex trace ID (W3C Trace Context).
func NewTraceID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on supported platforms; fall back to a UUID.
		return strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	return hex.EncodeToString(b[:])
}

// NewRequestID generates a unique request ID.
func NewRequestID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}

// ParseTraceparent extracts the trace ID from a W3C traceparent header
// ("00-<trace-id>-<parent-id>-<flags>"). ok is false for malformed values.
func ParseTraceparent(header string) (traceID string, ok bool) {
	parts := strings.Split(strings.TrimSpace(header), "-")
	if len(parts) != 4 {
		return "", false
	}
	version, traceID, parentID, flags := parts[0], parts[1], parts[2], parts[3]
	if len(version) != 2 || version == "ff" || !isLowerHex(version) {
		return "", false
	}
	if len(traceID) != 32 || !isLowerHex(traceID) || isAllZero(traceID) {
		return "", false
	}
	if len(parentID) != 16 || !isLowerHex(parentID) || isAllZero(parentID) {
		return "", false
	}
	if len(flags) != 2 || !isLowerHex(flags) {
		return "", false
	}
	return traceID, true
}

func isLowerHex(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func isAllZero(s string) bool {
	return strings.Trim(s, "0") == ""
}
