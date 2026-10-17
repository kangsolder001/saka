// Package logger wraps zerolog for structured JSON logging (spec §11.1).
//
// A *Logger is created once in the composition root (cmd/api, cmd/worker) and
// injected through constructors. Components call FromContext to get a derived
// logger that already carries trace_id, request_id, user_id and org_id.
package logger

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

func init() {
	// zerolog reads these package-level settings; route time through the
	// clock package so all timestamps are UTC and consistent.
	zerolog.TimeFieldFormat = "2006-01-02T15:04:05.000Z07:00"
	zerolog.TimestampFunc = clock.Real{}.Now
	zerolog.DurationFieldUnit = time.Millisecond
	zerolog.DurationFieldInteger = true
}

// Log field names shared with the rest of the system.
const (
	FieldTraceID   = "trace_id"
	FieldRequestID = "request_id"
	FieldUserID    = "user_id"
	FieldOrgID     = "org_id"
	FieldComponent = "component"
)

// Logger is the injectable application logger.
type Logger struct {
	zl zerolog.Logger
}

// New creates a JSON logger writing to w at the given level
// (debug, info, warn or error).
func New(w io.Writer, level string) (*Logger, error) {
	lvl, err := zerolog.ParseLevel(strings.ToLower(level))
	if err != nil || lvl == zerolog.NoLevel {
		return nil, fmt.Errorf("invalid log level %q", level)
	}
	return &Logger{zl: zerolog.New(w).Level(lvl).With().Timestamp().Logger()}, nil
}

// Nop returns a logger that discards everything, for tests.
func Nop() *Logger {
	return &Logger{zl: zerolog.Nop()}
}

// Component returns a logger that tags every entry with the component name
// (e.g. "auth", "outbox-worker").
func (l *Logger) Component(name string) *Logger {
	return &Logger{zl: l.zl.With().Str(FieldComponent, name).Logger()}
}

// Base returns the underlying logger without any request fields. Use it for
// startup and shutdown messages that have no request context.
func (l *Logger) Base() zerolog.Logger {
	return l.zl
}

// FromContext returns a logger that includes the identifiers stored in ctx.
// Fields not present in ctx are omitted.
func (l *Logger) FromContext(ctx context.Context) zerolog.Logger {
	c := l.zl.With()
	if id := reqctx.TraceID(ctx); id != "" {
		c = c.Str(FieldTraceID, id)
	}
	if id := reqctx.RequestID(ctx); id != "" {
		c = c.Str(FieldRequestID, id)
	}
	if id, ok := reqctx.UserID(ctx); ok {
		c = c.Str(FieldUserID, id.String())
	}
	if id, ok := reqctx.OrgID(ctx); ok {
		c = c.Str(FieldOrgID, id.String())
	}
	return c.Logger()
}
