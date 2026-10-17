package mail

import (
	"context"

	"github.com/kangsolder001/saka/internal/platform/logger"
)

// LogMailer writes emails to the log instead of sending them. It is meant
// for local development and tests only: the text body, which contains links
// with one-time tokens, is logged at debug level.
type LogMailer struct {
	log *logger.Logger
}

var _ Mailer = (*LogMailer)(nil)

// NewLogMailer creates a LogMailer.
func NewLogMailer(log *logger.Logger) *LogMailer {
	return &LogMailer{log: log.Component("mail-log")}
}

// Send logs the message metadata (and the text body at debug level).
func (m *LogMailer) Send(ctx context.Context, msg Message) error {
	if err := msg.Validate(); err != nil {
		return err
	}
	zl := m.log.FromContext(ctx)
	zl.Info().Str("to", msg.To).Str("subject", msg.Subject).Msg("email logged (not sent)")
	zl.Debug().Str("to", msg.To).Str("text", msg.Text).Msg("email body")
	return nil
}
