package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"
)

const (
	// ResendDefaultBaseURL is the Resend API root.
	ResendDefaultBaseURL = "https://api.resend.com"
	defaultResendTimeout = 15 * time.Second
	maxResendResponse    = 1 << 20
	maxResendErrorText   = 200
)

// ResendConfig describes the Resend account. APIKey is a secret: it lives in
// config.json only and is never logged or put in an error message.
type ResendConfig struct {
	APIKey string
	// From is the sender, e.g. "Saka <noreply@example.com>". Its domain must be
	// verified in Resend (SPF and DKIM, docs/email.md).
	From string
	// BaseURL defaults to ResendDefaultBaseURL.
	BaseURL string
	// Timeout bounds one API call when the context has no deadline of its own.
	Timeout time.Duration
	// HTTPClient replaces the default client (tests).
	HTTPClient *http.Client
}

// ResendMailer delivers messages through the Resend HTTP API (POST /emails),
// with the standard library only: no SDK.
type ResendMailer struct {
	apiKey  string
	from    string
	baseURL string
	client  *http.Client
	timeout time.Duration
}

var _ Mailer = (*ResendMailer)(nil)

// NewResendMailer validates the configuration and creates the mailer.
func NewResendMailer(cfg ResendConfig) (*ResendMailer, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("resend api key is required")
	}
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return nil, fmt.Errorf("invalid resend from address: %w", err)
	}
	base := cfg.BaseURL
	if base == "" {
		base = ResendDefaultBaseURL
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, errors.New("resend base url must be an http(s) URL")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultResendTimeout
	}
	return &ResendMailer{
		apiKey: cfg.APIKey, from: from.String(), baseURL: strings.TrimRight(base, "/"), client: client, timeout: timeout,
	}, nil
}

// ResendError is an answer from the Resend API other than success.
type ResendError struct {
	// Status is the HTTP status code.
	Status int
	// Name is Resend's error name (for example "validation_error"), when it sent one.
	Name string
	// Message is Resend's message, truncated.
	Message string
}

func (e *ResendError) Error() string {
	msg := fmt.Sprintf("resend: http %d", e.Status)
	if e.Name != "" {
		msg += " " + e.Name
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	return msg
}

// Permanent reports whether retrying the same message cannot succeed. Rejected
// requests (400, 404, 405, 422 and other 4xx) are the message's own fault.
// Everything that can heal is retried: 5xx and 429 (Resend is busy or rate
// limiting us), 408, 409 (a concurrent request) and 401/403, which are an API
// key or domain problem the operator may fix while the job is still retrying
// (like SMTP authentication failures).
func (e *ResendError) Permanent() bool {
	switch e.Status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusRequestTimeout, http.StatusConflict, http.StatusTooManyRequests:
		return false
	}
	return e.Status >= 400 && e.Status < 500
}

// IsPermanentResendError reports whether err is a Resend answer that retrying will not fix.
func IsPermanentResendError(err error) bool {
	var re *ResendError
	return errors.As(err, &re) && re.Permanent()
}

type resendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html,omitempty"`
	Text    string   `json:"text,omitempty"`
}

type resendErrorBody struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

// Send delivers msg. A network failure, a timeout, 429 and 5xx are returned as
// ordinary errors (the outbox worker retries them); a rejection that retrying
// cannot fix is a *ResendError with Permanent() true.
func (m *ResendMailer) Send(ctx context.Context, msg Message) error {
	if err := msg.Validate(); err != nil {
		return err
	}
	to, err := mail.ParseAddress(msg.To)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(resendRequest{
		From: m.from, To: []string{to.String()}, Subject: msg.Subject, HTML: msg.HTML, Text: msg.Text,
	})
	if err != nil {
		return fmt.Errorf("resend: encode request: %w", err)
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, m.timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/emails", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("resend: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "saka-mailer/1")

	resp, err := m.client.Do(req)
	if err != nil {
		// The URL error text never contains the Authorization header, but unwrap it
		// anyway so nothing but the cause (timeout, refused, reset) is reported.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return fmt.Errorf("resend: send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResendResponse))

	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		return nil
	}
	var parsed resendErrorBody
	_ = json.Unmarshal(body, &parsed) // best effort: an outage may answer with HTML
	return &ResendError{Status: resp.StatusCode, Name: truncate(parsed.Name, 64), Message: truncate(parsed.Message, maxResendErrorText)}
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit]
}
