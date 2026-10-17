package logger_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

func newLogger(t *testing.T, level string) (*logger.Logger, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	l, err := logger.New(buf, level)
	require.NoError(t, err)
	return l, buf
}

func decode(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var entry map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry), "log line must be valid JSON: %q", buf.String())
	return entry
}

func TestNew_InvalidLevel(t *testing.T) {
	for _, level := range []string{"", "verbose", "trace-ish"} {
		_, err := logger.New(&bytes.Buffer{}, level)
		assert.Error(t, err, level)
	}
}

func TestFromContext_IncludesAllIDs(t *testing.T) {
	l, buf := newLogger(t, "info")
	userID, orgID := uuid.New(), uuid.New()
	ctx := reqctx.WithTraceID(context.Background(), "trace-abc")
	ctx = reqctx.WithRequestID(ctx, "req-1")
	ctx = reqctx.WithUserID(ctx, userID)
	ctx = reqctx.WithOrgID(ctx, orgID)

	zl := l.FromContext(ctx)
	zl.Info().Msg("hello")

	entry := decode(t, buf)
	assert.Equal(t, "hello", entry["message"])
	assert.Equal(t, "info", entry["level"])
	assert.Equal(t, "trace-abc", entry[logger.FieldTraceID])
	assert.Equal(t, "req-1", entry[logger.FieldRequestID])
	assert.Equal(t, userID.String(), entry[logger.FieldUserID])
	assert.Equal(t, orgID.String(), entry[logger.FieldOrgID])
}

func TestFromContext_OmitsMissingIDs(t *testing.T) {
	l, buf := newLogger(t, "info")
	ctx := reqctx.WithTraceID(context.Background(), "trace-abc")

	zl := l.FromContext(ctx)
	zl.Info().Msg("hello")

	entry := decode(t, buf)
	assert.Equal(t, "trace-abc", entry[logger.FieldTraceID])
	assert.NotContains(t, entry, logger.FieldRequestID)
	assert.NotContains(t, entry, logger.FieldUserID)
	assert.NotContains(t, entry, logger.FieldOrgID)
}

func TestFromContext_DoesNotLeakBetweenContexts(t *testing.T) {
	l, buf := newLogger(t, "info")

	a := l.FromContext(reqctx.WithTraceID(context.Background(), "A"))
	b := l.FromContext(reqctx.WithTraceID(context.Background(), "B"))
	a.Info().Msg("a")
	b.Info().Msg("b")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 2)
	assert.Contains(t, lines[0], `"trace_id":"A"`)
	assert.Contains(t, lines[1], `"trace_id":"B"`)
}

func TestComponent(t *testing.T) {
	l, buf := newLogger(t, "info")

	zl := l.Component("auth").FromContext(reqctx.WithTraceID(context.Background(), "t"))
	zl.Warn().Msg("careful")

	entry := decode(t, buf)
	assert.Equal(t, "auth", entry[logger.FieldComponent])
	assert.Equal(t, "t", entry[logger.FieldTraceID])
}

func TestLevelFiltering(t *testing.T) {
	l, buf := newLogger(t, "warn")

	zl := l.FromContext(context.Background())
	zl.Info().Msg("hidden")
	zl.Error().Msg("shown")

	assert.NotContains(t, buf.String(), "hidden")
	assert.Contains(t, buf.String(), "shown")
}

func TestTimestampIsUTCMillis(t *testing.T) {
	l, buf := newLogger(t, "info")

	zl := l.Base()
	zl.Info().Msg("x")

	ts, ok := decode(t, buf)["time"].(string)
	require.True(t, ok)
	parsed, err := time.Parse("2006-01-02T15:04:05.000Z07:00", ts)
	require.NoError(t, err)
	assert.Equal(t, 0, offset(parsed))
}

func offset(t time.Time) int { _, o := t.Zone(); return o }

func TestNopDiscards(t *testing.T) {
	l := logger.Nop()

	zl := l.FromContext(context.Background())

	assert.NotPanics(t, func() { zl.Error().Msg("ignored") })
}
