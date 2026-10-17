//go:build integration

package mail_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/kangsolder001/saka/internal/mail"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/dbtest"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/outbox"
	"github.com/kangsolder001/saka/internal/platform/reqctx"
	"github.com/kangsolder001/saka/internal/platform/safego"
	emailtemplates "github.com/kangsolder001/saka/templates/email"
)

const mailpitImage = "axllent/mailpit:v1.31.6"

type mailpit struct {
	smtpHost string
	smtpPort int
	apiURL   string
}

func startMailpit(t *testing.T) mailpit {
	t.Helper()
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        mailpitImage,
			ExposedPorts: []string{"1025/tcp", "8025/tcp"},
			WaitingFor:   wait.ForHTTP("/readyz").WithPort("8025/tcp").WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	require.NoError(t, err)
	testcontainers.CleanupContainer(t, c)

	host, err := c.Host(ctx)
	require.NoError(t, err)
	smtpPort, err := c.MappedPort(ctx, "1025/tcp")
	require.NoError(t, err)
	apiPort, err := c.MappedPort(ctx, "8025/tcp")
	require.NoError(t, err)
	return mailpit{smtpHost: host, smtpPort: int(smtpPort.Num()), apiURL: "http://" + host + ":" + apiPort.Port()}
}

type mailpitMessage struct {
	ID      string
	Subject string
	To      []struct{ Address string }
	From    struct{ Address, Name string }
	Text    string
	HTML    string
}

func (m mailpit) messages(t *testing.T) []mailpitMessage {
	t.Helper()
	var list struct {
		Messages []struct{ ID string } `json:"messages"`
	}
	m.getJSON(t, "/api/v1/messages", &list)

	out := make([]mailpitMessage, 0, len(list.Messages))
	for _, item := range list.Messages {
		var msg mailpitMessage
		m.getJSON(t, "/api/v1/message/"+item.ID, &msg)
		out = append(out, msg)
	}
	return out
}

func (m mailpit) getJSON(t *testing.T, path string, dst any) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, m.apiURL+path, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	require.NoError(t, json.Unmarshal(body, dst))
}

type e2e struct {
	db      *db.DB
	store   *outbox.Store
	clock   *clock.Fake
	service *mail.Service
	logs    *bytes.Buffer
	log     *logger.Logger
}

func newE2E(t *testing.T) *e2e {
	t.Helper()
	dsn := dbtest.NewDatabase(t)
	dbtest.Migrate(t, dsn)
	cfg := config.Default()
	cfg.Database.URL = dsn
	d, err := db.Connect(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(d.Close)

	logs := &bytes.Buffer{}
	log, err := logger.New(logs, "debug")
	require.NoError(t, err)
	clk := clock.NewFake(time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC))
	store := outbox.NewStore(d.Pool(), clk)
	renderer, err := mail.NewRenderer(emailtemplates.FS, mail.AppInfo{Name: "Saka"})
	require.NoError(t, err)
	return &e2e{db: d, store: store, clock: clk, service: mail.NewService(renderer, store), logs: logs, log: log}
}

func (e *e2e) worker(t *testing.T, mailer mail.Mailer) *outbox.Worker {
	t.Helper()
	renderer, err := mail.NewRenderer(emailtemplates.FS, mail.AppInfo{Name: "Saka"})
	require.NoError(t, err)
	w, err := outbox.NewWorker(e.store, e.clock, e.log, safego.NewRunner(e.log), outbox.DefaultWorkerConfig())
	require.NoError(t, err)
	w.Register(mail.JobTypeSendEmail, mail.NewJobHandler(renderer, mailer, e.log).Handle)
	return w
}

func TestEmailFlow_RequestToMailpitThroughOutbox(t *testing.T) {
	mp := startMailpit(t)
	e := newE2E(t)
	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	requestCtx := reqctx.WithTraceID(context.Background(), traceID)

	// 1. The "request": a business change and the email job in ONE transaction.
	err := e.db.WithTx(requestCtx, func(tx pgx.Tx) error {
		return e.service.Send(requestCtx, tx, mail.Email{
			Template: "verify_email",
			To:       "ann@example.com",
			Data:     map[string]any{"name": "Ann", "url": "https://app.example/verify?token=abc123"},
		})
	})
	require.NoError(t, err)
	assert.Empty(t, mp.messages(t), "nothing is sent synchronously inside the request")

	// 2. The worker (a different process: no trace in its own context) delivers it.
	mailer, err := mail.NewSMTPMailer(mail.SMTPConfig{
		Host: mp.smtpHost, Port: mp.smtpPort, From: "Saka <noreply@saka.local>",
	}, e.clock)
	require.NoError(t, err)
	n, err := e.worker(t, mailer).RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, n)

	// 3. The email arrived, rendered, in Mailpit.
	msgs := mp.messages(t)
	require.Len(t, msgs, 1)
	msg := msgs[0]
	assert.Equal(t, "Verifikasi email Anda di Saka", msg.Subject)
	require.Len(t, msg.To, 1)
	assert.Equal(t, "ann@example.com", msg.To[0].Address)
	assert.Equal(t, "noreply@saka.local", msg.From.Address)
	assert.Contains(t, msg.Text, "Halo Ann")
	assert.Contains(t, msg.Text, "https://app.example/verify?token=abc123")
	assert.Contains(t, msg.HTML, `href="https://app.example/verify?token=abc123"`)

	// 4. The job is done and the request's trace id reached the worker's logs.
	var status string
	require.NoError(t, e.db.Pool().QueryRow(context.Background(), "SELECT status FROM outbox_jobs").Scan(&status))
	assert.Equal(t, "done", status)
	assert.Contains(t, e.logs.String(), `"trace_id":"`+traceID+`"`)
	assert.Contains(t, e.logs.String(), `"message":"email sent"`)
	assert.NotContains(t, e.logs.String(), "abc123", "one-time tokens must not be logged")
}

func TestEmailFlow_RollbackSendsNothing(t *testing.T) {
	mp := startMailpit(t)
	e := newE2E(t)
	ctx := context.Background()

	err := e.db.WithTx(ctx, func(tx pgx.Tx) error {
		if err := e.service.Send(ctx, tx, mail.Email{
			Template: "password_changed", To: "ann@example.com", Data: map[string]any{"name": "Ann"},
		}); err != nil {
			return err
		}
		return assert.AnError // the business change fails after the email was queued
	})
	require.Error(t, err)

	mailer, err := mail.NewSMTPMailer(mail.SMTPConfig{Host: mp.smtpHost, Port: mp.smtpPort, From: "Saka <noreply@saka.local>"}, e.clock)
	require.NoError(t, err)
	n, err := e.worker(t, mailer).RunOnce(ctx)

	require.NoError(t, err)
	assert.Zero(t, n, "rolled back: no job exists")
	assert.Empty(t, mp.messages(t), "and no email was sent")
}

func TestEmailFlow_SMTPDownIsRetriedThenDelivered(t *testing.T) {
	mp := startMailpit(t)
	e := newE2E(t)
	ctx := context.Background()
	require.NoError(t, e.db.WithTx(ctx, func(tx pgx.Tx) error {
		return e.service.Send(ctx, tx, mail.Email{
			Template: "password_changed", To: "ann@example.com", Data: map[string]any{"name": "Ann"},
		})
	}))

	// First attempt: the SMTP server is unreachable (port 1 is closed).
	down, err := mail.NewSMTPMailer(mail.SMTPConfig{Host: "127.0.0.1", Port: 1, From: "Saka <noreply@saka.local>"}, e.clock)
	require.NoError(t, err)
	_, err = e.worker(t, down).RunOnce(ctx)
	require.NoError(t, err)

	var status string
	var attempts int
	var lastErr *string
	require.NoError(t, e.db.Pool().QueryRow(ctx, "SELECT status, attempts, last_error FROM outbox_jobs").Scan(&status, &attempts, &lastErr))
	assert.Equal(t, "pending", status, "a transient failure is retried, not dropped")
	assert.Equal(t, 1, attempts)
	require.NotNil(t, lastErr)
	assert.Empty(t, mp.messages(t))

	// The server comes back; after the 1 minute backoff the retry delivers it.
	up, err := mail.NewSMTPMailer(mail.SMTPConfig{Host: mp.smtpHost, Port: mp.smtpPort, From: "Saka <noreply@saka.local>"}, e.clock)
	require.NoError(t, err)
	worker := e.worker(t, up)
	n, err := worker.RunOnce(ctx)
	require.NoError(t, err)
	assert.Zero(t, n, "not due yet")
	e.clock.Advance(time.Minute)
	n, err = worker.RunOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	assert.Len(t, mp.messages(t), 1)
	require.NoError(t, e.db.Pool().QueryRow(ctx, "SELECT status FROM outbox_jobs").Scan(&status))
	assert.Equal(t, "done", status)
}
