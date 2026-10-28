package mail_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/mail"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/outbox"
)

const resendKey = "re_test_KEY_0123456789abcdef"

type resendCall struct {
	method, path, auth, contentType string
	body                            map[string]any
}

type fakeResend struct {
	*httptest.Server
	mu    sync.Mutex
	calls []resendCall
}

func (f *fakeResend) last() resendCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

// newFakeResend answers every request with status and body and records it.
func newFakeResend(t *testing.T, status int, reply string) *fakeResend {
	t.Helper()
	f := &fakeResend{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.calls = append(f.calls, resendCall{
			method: r.Method, path: r.URL.Path, auth: r.Header.Get("Authorization"), contentType: r.Header.Get("Content-Type"), body: body,
		})
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(f.Close)
	return f
}

func newResend(t *testing.T, baseURL string) *mail.ResendMailer {
	t.Helper()
	m, err := mail.NewResendMailer(mail.ResendConfig{APIKey: resendKey, From: "Saka <noreply@example.com>", BaseURL: baseURL})
	require.NoError(t, err)
	return m
}

func newHandlerWith(t *testing.T, m mail.Mailer) *mail.JobHandler {
	t.Helper()
	return mail.NewJobHandler(realRenderer(t), m, logger.Nop())
}

var resendMessage = mail.Message{To: "ann@example.com", Subject: "Hi Ann", HTML: "<p>Hi</p>", Text: "Hi"}

func TestNewResendMailer_Validation(t *testing.T) {
	for name, cfg := range map[string]mail.ResendConfig{
		"no api key":   {From: "Saka <noreply@example.com>"},
		"blank key":    {APIKey: "  ", From: "Saka <noreply@example.com>"},
		"bad from":     {APIKey: resendKey, From: "nope"},
		"bad base url": {APIKey: resendKey, From: "a@example.com", BaseURL: "api.resend.com"},
	} {
		_, err := mail.NewResendMailer(cfg)
		require.Error(t, err, name)
		assert.NotContains(t, err.Error(), resendKey, name)
	}
	_, err := mail.NewResendMailer(mail.ResendConfig{APIKey: resendKey, From: "a@example.com"})
	require.NoError(t, err, "the base URL defaults to the real API")
}

func TestResendMailer_PostsTheMessageWithTheKeyAsBearerToken(t *testing.T) {
	srv := newFakeResend(t, http.StatusOK, `{"id":"49a3999c-0ce1-4ea6-ab68-afcd6dc2e794"}`)

	err := newResend(t, srv.URL).Send(context.Background(), resendMessage)

	require.NoError(t, err)
	call := srv.last()
	assert.Equal(t, http.MethodPost, call.method)
	assert.Equal(t, "/emails", call.path)
	assert.Equal(t, "Bearer "+resendKey, call.auth)
	assert.Equal(t, "application/json", call.contentType)
	assert.Equal(t, map[string]any{
		"from": "\"Saka\" <noreply@example.com>", "to": []any{"<ann@example.com>"}, "subject": "Hi Ann", "html": "<p>Hi</p>", "text": "Hi",
	}, call.body)
}

func TestResendMailer_TextOnlyMessagesOmitTheHTMLField(t *testing.T) {
	srv := newFakeResend(t, http.StatusOK, `{"id":"x"}`)

	err := newResend(t, srv.URL).Send(context.Background(), mail.Message{To: "ann@example.com", Subject: "Hi", Text: "plain"})

	require.NoError(t, err)
	assert.NotContains(t, srv.last().body, "html")
}

func TestResendMailer_RejectsAnInvalidMessageBeforeCallingTheAPI(t *testing.T) {
	srv := newFakeResend(t, http.StatusOK, `{}`)

	err := newResend(t, srv.URL).Send(context.Background(), mail.Message{To: "ann@example.com\r\nBcc: x@example.com", Subject: "s", Text: "t"})

	require.Error(t, err)
	assert.Empty(t, srv.calls)
}

func TestResendMailer_ErrorsAreClassifiedForTheOutboxWorker(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		body          string
		wantPermanent bool
	}{
		{"validation error", 422, `{"statusCode":422,"name":"validation_error","message":"Invalid ` + "`to`" + ` field."}`, true},
		{"bad request", 400, `{"name":"invalid_parameter","message":"nope"}`, true},
		{"unverified domain", 403, `{"name":"validation_error","message":"The example.com domain is not verified."}`, false},
		{"invalid api key", 401, `{"name":"missing_api_key","message":"Missing API key."}`, false},
		{"rate limited", 429, `{"name":"rate_limit_exceeded","message":"Too many requests."}`, false},
		{"concurrent request", 409, `{"name":"concurrent_idempotent_requests","message":"busy"}`, false},
		{"server error", 500, `{"name":"application_error","message":"boom"}`, false},
		{"bad gateway with html", 502, `<html>bad gateway</html>`, false},
		{"service unavailable", 503, ``, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newFakeResend(t, tt.status, tt.body)

			err := newResend(t, srv.URL).Send(context.Background(), resendMessage)

			var re *mail.ResendError
			require.ErrorAs(t, err, &re)
			assert.Equal(t, tt.status, re.Status)
			assert.Equal(t, tt.wantPermanent, mail.IsPermanentResendError(err))
			assert.NotContains(t, err.Error(), resendKey)
		})
	}
}

func TestResendMailer_ANetworkFailureIsRetryableAndHidesTheKey(t *testing.T) {
	srv := newFakeResend(t, http.StatusOK, `{}`)
	m := newResend(t, srv.URL)
	srv.Close()

	err := m.Send(context.Background(), resendMessage)

	require.Error(t, err)
	assert.False(t, mail.IsPermanentResendError(err), "a refused connection heals; retry it")
	assert.NotContains(t, err.Error(), resendKey)
}

func TestResendMailer_HonoursTheContextDeadline(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(block); srv.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := newResend(t, srv.URL).Send(ctx, resendMessage)

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded), err.Error())
	assert.False(t, mail.IsPermanentResendError(err))
}

func TestResendMailer_AnErrorMessageFromResendIsTruncated(t *testing.T) {
	srv := newFakeResend(t, http.StatusUnprocessableEntity, `{"name":"validation_error","message":"`+strings.Repeat("x", 5000)+`"}`)

	err := newResend(t, srv.URL).Send(context.Background(), resendMessage)

	require.Error(t, err)
	assert.Less(t, len(err.Error()), 400)
}

// The job handler turns the classification into the outbox's retry decision.
func TestJobHandler_ResendFailuresAreRetriedOrFailedForGood(t *testing.T) {
	for name, tc := range map[string]struct {
		status        int
		wantPermanent bool
	}{
		"rejected address":   {http.StatusUnprocessableEntity, true},
		"resend is down":     {http.StatusBadGateway, false},
		"we are throttled":   {http.StatusTooManyRequests, false},
		"key not authorized": {http.StatusForbidden, false},
	} {
		t.Run(name, func(t *testing.T) {
			srv := newFakeResend(t, tc.status, `{"name":"x","message":"y"}`)
			h := newHandlerWith(t, newResend(t, srv.URL))

			err := h.Handle(context.Background(), payload(t, "verify_email", "a@example.com", verifyData))

			require.Error(t, err)
			assert.Equal(t, tc.wantPermanent, outbox.IsPermanent(err))
			assert.NotContains(t, err.Error(), resendKey)
		})
	}
}

func TestJobHandler_ResendDeliversTheRenderedTemplate(t *testing.T) {
	srv := newFakeResend(t, http.StatusOK, `{"id":"x"}`)
	h := newHandlerWith(t, newResend(t, srv.URL))

	require.NoError(t, h.Handle(context.Background(), payload(t, "verify_email", "a@example.com", verifyData)))

	call := srv.last()
	assert.Equal(t, []any{"<a@example.com>"}, call.body["to"])
	assert.NotEmpty(t, call.body["subject"])
	assert.Contains(t, call.body["html"], "https://app.example/verify?token=abc")
}
