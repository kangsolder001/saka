package reqctx_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

func TestEmptyContext(t *testing.T) {
	ctx := context.Background()

	assert.Empty(t, reqctx.TraceID(ctx))
	assert.Empty(t, reqctx.RequestID(ctx))
	_, hasUser := reqctx.UserID(ctx)
	_, hasOrg := reqctx.OrgID(ctx)
	assert.False(t, hasUser)
	assert.False(t, hasOrg)
}

func TestRoundTrip(t *testing.T) {
	userID, orgID := uuid.New(), uuid.New()

	ctx := reqctx.WithTraceID(context.Background(), "trace-1")
	ctx = reqctx.WithRequestID(ctx, "req-1")
	ctx = reqctx.WithUserID(ctx, userID)
	ctx = reqctx.WithOrgID(ctx, orgID)
	sessionID := uuid.New()
	ctx = reqctx.WithSessionID(ctx, sessionID)

	gotSession, ok := reqctx.SessionID(ctx)
	require.True(t, ok)
	assert.Equal(t, sessionID, gotSession)
	assert.Equal(t, "trace-1", reqctx.TraceID(ctx))
	assert.Equal(t, "req-1", reqctx.RequestID(ctx))
	gotUser, ok := reqctx.UserID(ctx)
	require.True(t, ok)
	assert.Equal(t, userID, gotUser)
	gotOrg, ok := reqctx.OrgID(ctx)
	require.True(t, ok)
	assert.Equal(t, orgID, gotOrg)
}

func TestValuesDoNotCollideWithStringKeys(t *testing.T) {
	ctx := context.WithValue(context.Background(), "trace_id", "spoofed") //nolint:staticcheck // proves keys are private

	assert.Empty(t, reqctx.TraceID(ctx))
}

func TestNewTraceID(t *testing.T) {
	a, b := reqctx.NewTraceID(), reqctx.NewTraceID()

	assert.Len(t, a, 32)
	assert.Regexp(t, `^[0-9a-f]{32}$`, a)
	assert.NotEqual(t, a, b)
}

func TestNewRequestID(t *testing.T) {
	a, b := reqctx.NewRequestID(), reqctx.NewRequestID()

	_, err := uuid.Parse(a)
	require.NoError(t, err)
	assert.NotEqual(t, a, b)
}

func TestParseTraceparent(t *testing.T) {
	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"

	tests := []struct {
		name   string
		header string
		wantOK bool
	}{
		{"valid", "00-" + traceID + "-00f067aa0ba902b7-01", true},
		{"valid with surrounding spaces", " 00-" + traceID + "-00f067aa0ba902b7-00 ", true},
		{"empty", "", false},
		{"too few parts", "00-" + traceID + "-01", false},
		{"uppercase hex", "00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01", false},
		{"short trace id", "00-4bf92f35-00f067aa0ba902b7-01", false},
		{"all zero trace id", "00-00000000000000000000000000000000-00f067aa0ba902b7-01", false},
		{"all zero parent id", "00-" + traceID + "-0000000000000000-01", false},
		{"forbidden version", "ff-" + traceID + "-00f067aa0ba902b7-01", false},
		{"non hex flags", "00-" + traceID + "-00f067aa0ba902b7-zz", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := reqctx.ParseTraceparent(tt.header)

			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, traceID, got)
			} else {
				assert.Empty(t, got)
			}
		})
	}
}
