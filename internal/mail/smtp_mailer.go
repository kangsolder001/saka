package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kangsolder001/saka/internal/platform/clock"
)

const defaultSMTPTimeout = 30 * time.Second

// SMTPConfig describes the SMTP relay. Username may be empty for relays
// without authentication (Mailpit in development).
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	// From is the sender, e.g. "Saka <noreply@example.com>".
	From string
}

// SMTPMailer delivers messages through an SMTP server. It upgrades to TLS
// with STARTTLS when the server offers it.
type SMTPMailer struct {
	cfg  SMTPConfig
	from *mail.Address
	clk  clock.Clock
}

var _ Mailer = (*SMTPMailer)(nil)

// NewSMTPMailer validates the configuration and creates the mailer.
func NewSMTPMailer(cfg SMTPConfig, clk clock.Clock) (*SMTPMailer, error) {
	if cfg.Host == "" || cfg.Port <= 0 {
		return nil, errors.New("smtp host and port are required")
	}
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return nil, fmt.Errorf("invalid smtp from address: %w", err)
	}
	return &SMTPMailer{cfg: cfg, from: from, clk: clk}, nil
}

// Send delivers msg. The context deadline bounds the whole exchange; without
// one a 30 second limit applies.
func (m *SMTPMailer) Send(ctx context.Context, msg Message) error {
	if err := msg.Validate(); err != nil {
		return err
	}
	raw, err := m.build(msg)
	if err != nil {
		return fmt.Errorf("build message: %w", err)
	}
	to, err := mail.ParseAddress(msg.To)
	if err != nil {
		return err
	}

	// I/O deadlines are wall-clock by nature, so they come from the context
	// (real time), never from the injectable Clock, which tests move around.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultSMTPTimeout)
		defer cancel()
	}
	deadline, _ := ctx.Deadline()

	addr := net.JoinHostPort(m.cfg.Host, fmt.Sprint(m.cfg.Port))
	dialer := net.Dialer{Deadline: deadline}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("connect to smtp server: %w", err)
	}
	// A connection deadline instead of a watcher goroutine: every read and
	// write fails once the deadline passes, so nothing can hang or leak.
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return fmt.Errorf("set smtp deadline: %w", err)
	}

	client, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp handshake: %w", err)
	}
	defer func() { _ = client.Close() }()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	if m.cfg.Username != "" {
		auth := smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := client.Mail(m.from.Address); err != nil {
		return fmt.Errorf("smtp MAIL FROM: %w", err)
	}
	if err := client.Rcpt(to.Address); err != nil {
		return fmt.Errorf("smtp RCPT TO: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		_ = w.Close()
		return fmt.Errorf("smtp write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp finish body: %w", err)
	}
	// The message is already accepted; a failing QUIT is not a delivery failure.
	_ = client.Quit()
	return nil
}

// IsPermanentSMTPError reports whether err is an SMTP reply that retrying
// will not fix (5xx, such as an unknown mailbox). Authentication failures
// (530, 534, 535) are excluded: they are a configuration problem that may be
// fixed while the job is still being retried.
func IsPermanentSMTPError(err error) bool {
	var tpErr *textproto.Error
	if !errors.As(err, &tpErr) {
		return false
	}
	switch tpErr.Code {
	case 530, 534, 535:
		return false
	}
	return tpErr.Code >= 500 && tpErr.Code < 600
}

// build renders the RFC 5322 message: multipart/alternative with plain text
// first and HTML last (clients prefer the last part they understand).
func (m *SMTPMailer) build(msg Message) ([]byte, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	if msg.Text != "" {
		if err := writeQPPart(mw, "text/plain; charset=utf-8", msg.Text); err != nil {
			return nil, err
		}
	}
	if msg.HTML != "" {
		if err := writeQPPart(mw, "text/html; charset=utf-8", msg.HTML); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}

	domain := m.from.Address[strings.LastIndex(m.from.Address, "@")+1:]
	var out bytes.Buffer
	headers := []struct{ k, v string }{
		{"From", m.from.String()},
		{"To", msg.To},
		{"Subject", mime.QEncoding.Encode("utf-8", msg.Subject)},
		{"Date", m.clk.Now().Format(time.RFC1123Z)},
		{"Message-ID", "<" + uuid.NewString() + "@" + domain + ">"},
		{"MIME-Version", "1.0"},
		{"Content-Type", "multipart/alternative; boundary=" + mw.Boundary()},
	}
	for _, h := range headers {
		out.WriteString(h.k + ": " + h.v + "\r\n")
	}
	out.WriteString("\r\n")
	out.Write(body.Bytes())
	return out.Bytes(), nil
}

func writeQPPart(mw *multipart.Writer, contentType, content string) error {
	h := textproto.MIMEHeader{}
	h.Set("Content-Type", contentType)
	h.Set("Content-Transfer-Encoding", "quoted-printable")
	part, err := mw.CreatePart(h)
	if err != nil {
		return err
	}
	qp := quotedprintable.NewWriter(part)
	if _, err := qp.Write([]byte(content)); err != nil {
		return err
	}
	return qp.Close()
}
