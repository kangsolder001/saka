// Package mail renders transactional emails and delivers them through a
// Mailer. Services never send mail directly: they enqueue an outbox job in
// their own transaction (see Service) and the worker delivers it.
package mail

import (
	"context"
	"errors"
	"fmt"
	netmail "net/mail"
	"strings"
)

// Message is a fully rendered email ready to deliver.
type Message struct {
	To      string
	Subject string
	HTML    string
	Text    string
}

// Validate checks the fields that must never reach a transport malformed.
// Header values with line breaks are rejected to prevent header injection.
func (m Message) Validate() error {
	if _, err := netmail.ParseAddress(m.To); err != nil {
		return fmt.Errorf("invalid recipient address: %w", err)
	}
	if strings.ContainsAny(m.To, "\r\n") || strings.ContainsAny(m.Subject, "\r\n") {
		return errors.New("recipient and subject must not contain line breaks")
	}
	if strings.TrimSpace(m.Subject) == "" {
		return errors.New("subject is required")
	}
	if m.HTML == "" && m.Text == "" {
		return errors.New("message needs an HTML or text body")
	}
	return nil
}

// Mailer delivers one message. Implementations must be safe for concurrent
// use and respect the context deadline.
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}
