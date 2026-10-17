package mail_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/mail"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
)

func validMessage() mail.Message {
	return mail.Message{To: "user@example.com", Subject: "Hello", HTML: "<p>hi</p>", Text: "hi"}
}

func TestMessageValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(m *mail.Message)
		wantErr string
	}{
		{"valid", func(*mail.Message) {}, ""},
		{"named recipient", func(m *mail.Message) { m.To = "Ann <ann@example.com>" }, ""},
		{"bad recipient", func(m *mail.Message) { m.To = "not-an-email" }, "invalid recipient"},
		{"empty recipient", func(m *mail.Message) { m.To = "" }, "invalid recipient"},
		{"header injection in subject", func(m *mail.Message) { m.Subject = "Hi\r\nBcc: evil@example.com" }, "line breaks"},
		{"header injection in recipient", func(m *mail.Message) { m.To = "a@example.com\r\nBcc: evil@example.com" }, "invalid recipient"},
		{"blank subject", func(m *mail.Message) { m.Subject = "  " }, "subject is required"},
		{"no body", func(m *mail.Message) { m.HTML, m.Text = "", "" }, "needs an HTML or text body"},
		{"text only is fine", func(m *mail.Message) { m.HTML = "" }, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := validMessage()
			tt.mutate(&m)

			err := m.Validate()

			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestLogMailer_LogsMetadataWithTraceID(t *testing.T) {
	buf := &bytes.Buffer{}
	log, err := logger.New(buf, "info")
	require.NoError(t, err)
	ctx := reqctx.WithTraceID(context.Background(), "trace-mail")

	err = mail.NewLogMailer(log).Send(ctx, validMessage())

	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, `"to":"user@example.com"`)
	assert.Contains(t, out, `"subject":"Hello"`)
	assert.Contains(t, out, `"trace_id":"trace-mail"`)
	assert.NotContains(t, out, `"text":"hi"`, "body is only logged at debug level")
}

func TestLogMailer_BodyOnlyAtDebugLevel(t *testing.T) {
	buf := &bytes.Buffer{}
	log, err := logger.New(buf, "debug")
	require.NoError(t, err)

	require.NoError(t, mail.NewLogMailer(log).Send(context.Background(), validMessage()))

	assert.True(t, strings.Contains(buf.String(), `"text":"hi"`))
}

func TestLogMailer_RejectsInvalidMessage(t *testing.T) {
	m := validMessage()
	m.To = "nope"

	err := mail.NewLogMailer(logger.Nop()).Send(context.Background(), m)

	assert.Error(t, err)
}
