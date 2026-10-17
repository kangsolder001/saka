package mail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	netmail "net/mail"

	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/outbox"
)

// JobTypeSendEmail is the outbox job type handled by JobHandler.
const JobTypeSendEmail = "email.send"

// Email is a request to send a templated email.
type Email struct {
	// Template is the template code, e.g. "verify_email".
	Template string
	// To is the recipient address.
	To string
	// Data fills the template ({{.Data.<key>}}); keys are snake_case.
	Data map[string]any
}

// sendPayload is the JSON stored in the job; it must keep the same fields as Email
// so the conversion in Send stays valid.
type sendPayload struct {
	Template string         `json:"template"`
	To       string         `json:"to"`
	Data     map[string]any `json:"data"`
}

// Sender is what other modules depend on to send email.
type Sender interface {
	// Send enqueues the email inside the caller's transaction: it is delivered
	// if and only if that transaction commits. Nothing is sent synchronously.
	Send(ctx context.Context, tx db.DBTX, email Email) error
}

// Service implements Sender on top of the outbox.
type Service struct {
	renderer *Renderer
	enqueuer outbox.Enqueuer
}

var _ Sender = (*Service)(nil)

// NewService creates a Service. The renderer is used to reject bad requests
// at enqueue time, while the caller's transaction can still be rolled back.
func NewService(renderer *Renderer, enqueuer outbox.Enqueuer) *Service {
	return &Service{renderer: renderer, enqueuer: enqueuer}
}

// Send validates the request and inserts an email.send outbox job using tx.
// An unknown template, a missing template value or an invalid recipient is a
// programming error and fails here, rolling back the caller's work, instead
// of silently failing in the background later.
func (s *Service) Send(ctx context.Context, tx db.DBTX, email Email) error {
	if _, err := netmail.ParseAddress(email.To); err != nil {
		return fmt.Errorf("mail: invalid recipient: %w", err)
	}
	if _, err := s.renderer.Render(email.Template, email.Data); err != nil {
		return fmt.Errorf("mail: %w", err)
	}

	_, err := s.enqueuer.Enqueue(ctx, tx, outbox.NewJob{
		Type:    JobTypeSendEmail,
		Payload: sendPayload(email),
	})
	if err != nil {
		return fmt.Errorf("mail: enqueue %s: %w", email.Template, err)
	}
	return nil
}

// JobHandler delivers email.send outbox jobs through a Mailer.
type JobHandler struct {
	renderer *Renderer
	mailer   Mailer
	log      *logger.Logger
}

// NewJobHandler creates the handler to register on the outbox worker.
func NewJobHandler(renderer *Renderer, mailer Mailer, log *logger.Logger) *JobHandler {
	return &JobHandler{renderer: renderer, mailer: mailer, log: log.Component("mail")}
}

// Handle renders and sends one email. Failures that retrying cannot fix
// (malformed payload, unknown template, rejected recipient) are returned as
// permanent so the job fails immediately; everything else is retried.
func (h *JobHandler) Handle(ctx context.Context, job outbox.Job) error {
	var p sendPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return outbox.Permanent(fmt.Errorf("decode email payload: %w", err))
	}

	rendered, err := h.renderer.Render(p.Template, p.Data)
	if err != nil {
		return outbox.Permanent(err)
	}
	msg := Message{To: p.To, Subject: rendered.Subject, HTML: rendered.HTML, Text: rendered.Text}
	if err := msg.Validate(); err != nil {
		return outbox.Permanent(err)
	}

	if err := h.mailer.Send(ctx, msg); err != nil {
		if IsPermanentSMTPError(err) || errors.Is(err, ErrUnknownTemplate) {
			return outbox.Permanent(err)
		}
		return fmt.Errorf("send %s email: %w", p.Template, err)
	}

	// The recipient address is personal data; log the template only.
	zl := h.log.FromContext(ctx)
	zl.Info().Str("template", p.Template).Msg("email sent")
	return nil
}
