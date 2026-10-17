package mail_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/mail"
	mailmocks "github.com/kangsolder001/saka/internal/mail/mocks"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/outbox"
	outboxmocks "github.com/kangsolder001/saka/internal/platform/outbox/mocks"
)

var verifyData = map[string]any{"name": "Ann", "url": "https://app.example/verify?token=abc"}

func TestService_SendEnqueuesJobInCallersTransaction(t *testing.T) {
	enq := outboxmocks.NewMockEnqueuer(t)
	svc := mail.NewService(realRenderer(t), enq)
	enq.EXPECT().Enqueue(mock.Anything, mock.Anything, mock.MatchedBy(func(j outbox.NewJob) bool {
		raw, err := json.Marshal(j.Payload)
		return err == nil && j.Type == mail.JobTypeSendEmail && jsonEq(raw,
			`{"template":"verify_email","to":"ann@example.com","data":{"name":"Ann","url":"https://app.example/verify?token=abc"}}`)
	})).Return(uuid.New(), nil).Once()

	err := svc.Send(context.Background(), &nilTx{}, mail.Email{Template: "verify_email", To: "ann@example.com", Data: verifyData})

	require.NoError(t, err)
}

func jsonEq(a []byte, b string) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return false
	}
	ax, _ := json.Marshal(x)
	by, _ := json.Marshal(y)
	return string(ax) == string(by)
}

func TestService_SendRejectsBadRequestsWithoutEnqueueing(t *testing.T) {
	tests := map[string]mail.Email{
		"unknown template": {Template: "nope", To: "a@example.com", Data: verifyData},
		"missing data":     {Template: "verify_email", To: "a@example.com", Data: map[string]any{"name": "Ann"}},
		"bad recipient":    {Template: "verify_email", To: "not-an-email", Data: verifyData},
		"empty recipient":  {Template: "verify_email", To: "", Data: verifyData},
	}
	for name, email := range tests {
		t.Run(name, func(t *testing.T) {
			enq := outboxmocks.NewMockEnqueuer(t) // no expectation: Enqueue must not be called
			svc := mail.NewService(realRenderer(t), enq)

			err := svc.Send(context.Background(), &nilTx{}, email)

			assert.Error(t, err)
		})
	}
}

func TestService_SendPropagatesEnqueueFailure(t *testing.T) {
	enq := outboxmocks.NewMockEnqueuer(t)
	boom := errors.New("insert failed")
	enq.EXPECT().Enqueue(mock.Anything, mock.Anything, mock.Anything).Return(uuid.Nil, boom).Once()
	svc := mail.NewService(realRenderer(t), enq)

	err := svc.Send(context.Background(), &nilTx{}, mail.Email{Template: "verify_email", To: "a@example.com", Data: verifyData})

	assert.ErrorIs(t, err, boom, "the caller's transaction must roll back")
}

func payload(t *testing.T, template, to string, data map[string]any) outbox.Job {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"template": template, "to": to, "data": data})
	require.NoError(t, err)
	return outbox.Job{ID: uuid.New(), Type: mail.JobTypeSendEmail, Payload: raw, Attempts: 1}
}

func newHandler(t *testing.T) (*mail.JobHandler, *mailmocks.MockMailer) {
	t.Helper()
	m := mailmocks.NewMockMailer(t)
	return mail.NewJobHandler(realRenderer(t), m, logger.Nop()), m
}

func TestJobHandler_RendersAndSends(t *testing.T) {
	h, m := newHandler(t)
	m.EXPECT().Send(mock.Anything, mock.MatchedBy(func(msg mail.Message) bool {
		return msg.To == "ann@example.com" &&
			msg.Subject == "Verifikasi email Anda di Saka" &&
			len(msg.HTML) > 0 && len(msg.Text) > 0 &&
			strings.Contains(msg.Text, "https://app.example/verify?token=abc")
	})).Return(nil).Once()

	err := h.Handle(context.Background(), payload(t, "verify_email", "ann@example.com", verifyData))

	require.NoError(t, err)
}

func TestJobHandler_PermanentFailures(t *testing.T) {
	garbage := outbox.Job{ID: uuid.New(), Payload: json.RawMessage(`{not json`), Attempts: 1}
	tests := map[string]outbox.Job{
		"malformed payload": garbage,
		"unknown template":  payload(t, "nope", "a@example.com", nil),
		"missing data":      payload(t, "verify_email", "a@example.com", map[string]any{"name": "Ann"}),
		"invalid recipient": payload(t, "verify_email", "nope", verifyData),
	}
	for name, job := range tests {
		t.Run(name, func(t *testing.T) {
			h, _ := newHandler(t) // the mailer must not be called

			err := h.Handle(context.Background(), job)

			require.Error(t, err)
			assert.True(t, outbox.IsPermanent(err), "retrying cannot fix this: %v", err)
		})
	}
}

func TestJobHandler_MailerFailureClassification(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		wantPermanent bool
	}{
		{"recipient rejected", smtpError(550), true},
		{"server busy", smtpError(451), false},
		{"network error", errors.New("connection reset"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, m := newHandler(t)
			m.EXPECT().Send(mock.Anything, mock.Anything).Return(tt.err).Once()

			err := h.Handle(context.Background(), payload(t, "verify_email", "a@example.com", verifyData))

			require.Error(t, err)
			assert.Equal(t, tt.wantPermanent, outbox.IsPermanent(err))
			assert.ErrorIs(t, err, tt.err)
		})
	}
}
