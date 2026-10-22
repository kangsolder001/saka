//go:build integration

package app_test

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/app"
	"github.com/kangsolder001/saka/internal/billing/payment"
	"github.com/kangsolder001/saka/internal/billing/payment/fake"
	"github.com/kangsolder001/saka/internal/billing/payment/midtrans"
	"github.com/kangsolder001/saka/internal/mail"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/outbox"
	"github.com/kangsolder001/saka/internal/platform/safego"
	"github.com/kangsolder001/saka/internal/platform/scheduler"
	emailtemplates "github.com/kangsolder001/saka/templates/email"
)

// These tests back the observability audit (spec §11.1, §11.4): the IDs show
// up in the logs of every module and process, failures carry context, and no
// secret ever reaches a log line.

const traceA = "4bf92f3577b34da6a3ce929d0e0e4736"

func traceparentFor(trace string) map[string]string {
	return map[string]string{"traceparent": "00-" + trace + "-00f067aa0ba902b7-01"}
}

func withHeaders(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// logLines returns the captured entries whose message is msg.
func (s *stack) logLines(msg string) []map[string]any {
	s.t.Helper()
	var out []map[string]any
	for _, e := range s.logs.entries(s.t) {
		if e["message"] == msg {
			out = append(out, e)
		}
	}
	return out
}

// requestLog finds the access log of the request with the given trace and route.
func (s *stack) requestLog(trace, route string) map[string]any {
	s.t.Helper()
	for _, e := range s.logLines("request completed") {
		if e["trace_id"] == trace && e["route"] == route {
			return e
		}
	}
	s.t.Fatalf("no access log for trace %s route %s in:\n%s", trace, route, s.logs.String())
	return nil
}

func TestObservability_HTTPLogsCarryTraceRequestUserAndOrgIDs(t *testing.T) {
	s := newStack(t)
	tm := s.newTeam()
	ownerID := s.userID("owner@example.com")

	res := s.get(orgPath(tm.orgID), withHeaders(bearer(tm.owner), traceparentFor(traceA)))
	require.Equal(t, http.StatusOK, res.Code)

	line := s.requestLog(traceA, "/api/v1/orgs/:orgId")
	assert.Equal(t, res.Header().Get("X-Request-ID"), line["request_id"])
	assert.Equal(t, ownerID, line["user_id"], "the authenticated user is on the access log")
	assert.Equal(t, tm.orgID, line["org_id"], "the tenant is on the access log")
	assert.Equal(t, "http", line["component"])
	assert.NotContains(t, line, "path", "the raw URL is never logged, only the route template")

	anonymous := s.get("/api/v1/plans", traceparentFor("0af7651916cd43dd8448eb211c80319c"))
	require.Equal(t, http.StatusOK, anonymous.Code)
	anon := s.requestLog("0af7651916cd43dd8448eb211c80319c", "/api/v1/plans")
	assert.NotContains(t, anon, "user_id")
	assert.NotContains(t, anon, "org_id")
	assert.NotEmpty(t, anon["request_id"])
}

func TestObservability_ServiceLogsShareTheTraceOfTheRequestThatCausedThem(t *testing.T) {
	s := newStack(t)
	s.registered("ann@example.com")
	login := s.cookieLogin("ann@example.com")
	_, rotated := s.refreshWithCookie(login.refresh)
	require.NotEmpty(t, rotated.refresh)

	// Replaying the rotated token is a theft signal that the auth service logs.
	replay := s.post(refreshPath, ``, withHeaders(cookieHeader(login.refresh), traceparentFor(traceA)))
	require.Equal(t, http.StatusUnauthorized, replay.Code)

	lines := s.logLines("refresh token reuse detected; session revoked")
	require.Len(t, lines, 1)
	assert.Equal(t, traceA, lines[0]["trace_id"], "the service log joins the request's trace")
	assert.Equal(t, replay.Header().Get("X-Request-ID"), lines[0]["request_id"])
	assert.NotContains(t, lines[0], "user_id", "the reused token is unauthenticated: nothing to attribute yet, and no token is logged")
}

func TestObservability_BillingLogsCarryTheOrganizationFromCheckoutThroughTheWebhook(t *testing.T) {
	s := newStack(t)
	tm := s.billingTeam()
	number := s.buyPlan(tm, "pro")

	hook := s.do(http.MethodPost, webhookPath, string(func() []byte { b, _ := fake.Webhook(paid("evt-1", number, 99000)); return b }()),
		withHeaders(fakeSignatureHeader(paid("evt-1", number, 99000)), traceparentFor(traceA)))
	require.Equal(t, http.StatusOK, hook.Code, hook.Body.String())

	issued := s.logLines("invoice issued")
	require.Len(t, issued, 1)
	assert.Equal(t, tm.orgID, issued[0]["org_id"])
	assert.NotEmpty(t, issued[0]["user_id"], "the owner who checked out")
	assert.NotEmpty(t, issued[0]["trace_id"])

	payments := s.logLines("invoice paid")
	require.Len(t, payments, 1)
	assert.Equal(t, traceA, payments[0]["trace_id"], "the webhook request's trace")
	assert.Equal(t, tm.orgID, payments[0]["org_id"], "a webhook has no tenant on the request, the log adds the invoice's")
	assert.Equal(t, number, payments[0]["invoice_number"])
	// Rejected notifications are logged with the same trace but never the signature.
	bad := s.do(http.MethodPost, webhookPath, `{}`, withHeaders(map[string]string{fake.SignatureHeader: "deadbeef"}, traceparentFor(traceA)))
	require.Equal(t, http.StatusUnauthorized, bad.Code)
	rejected := s.logLines("webhook rejected: invalid signature")
	require.Len(t, rejected, 1)
	assert.Equal(t, traceA, rejected[0]["trace_id"])
	assert.Equal(t, "fake", rejected[0]["provider"])
}

func fakeSignatureHeader(ev payment.WebhookEvent) map[string]string {
	_, header := fake.Webhook(ev)
	return map[string]string{fake.SignatureHeader: header.Get(fake.SignatureHeader)}
}

// recordingMailer records what it would send, or fails with err.
type recordingMailer struct {
	mu   sync.Mutex
	sent []mail.Message
	err  error
}

func (m *recordingMailer) Send(_ context.Context, msg mail.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, msg)
	return nil
}

// emailWorker is the worker process in miniature: the real outbox worker and
// email handler on the stack's database, logger and clock.
func (s *stack) emailWorker(mailer mail.Mailer) *outbox.Worker {
	s.t.Helper()
	renderer, err := mail.NewRenderer(emailtemplates.FS, mail.AppInfo{Name: "Saka"})
	require.NoError(s.t, err)
	worker, err := outbox.NewWorker(outbox.NewStore(s.db.Pool(), s.clock), s.clock, s.log, safego.NewRunner(s.log), outbox.DefaultWorkerConfig())
	require.NoError(s.t, err)
	worker.Register(mail.JobTypeSendEmail, mail.NewJobHandler(renderer, mailer, s.log).Handle)
	return worker
}

func TestObservability_OutboxJobLogsRestoreTheRequestTraceAndFailuresCarryContext(t *testing.T) {
	s := newStack(t)
	res := s.post(registerPath, registerBody("ann@example.com"), traceparentFor(traceA))
	require.Equal(t, http.StatusCreated, res.Code)

	// A transient failure is retried and logged with everything needed to find the job.
	failing := &recordingMailer{err: errors.New("smtp: connection refused")}
	n, err := s.emailWorker(failing).RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, n)
	retry := s.logLines("outbox job failed, will retry")
	require.Len(t, retry, 1)
	assert.Equal(t, traceA, retry[0]["trace_id"], "the worker restores the trace of the request that enqueued the job")
	assert.Equal(t, "email.send", retry[0]["job_type"])
	assert.NotEmpty(t, retry[0]["job_id"])
	assert.EqualValues(t, 1, retry[0]["attempt"])
	assert.Contains(t, retry[0]["error"], "connection refused")
	assert.NotEmpty(t, retry[0]["next_attempt_at"])
	assert.NotEmpty(t, retry[0]["request_id"], "the worker's own request id for this attempt")

	// Once the backoff has passed the job is delivered and says so.
	s.clock.Advance(2 * time.Minute) // more than the first retry delay (1m)
	ok := &recordingMailer{}
	n, err = s.emailWorker(ok).RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, n)
	done := s.logLines("outbox job done")
	require.Len(t, done, 1)
	assert.Equal(t, traceA, done[0]["trace_id"])
	assert.EqualValues(t, 2, done[0]["attempt"])
	sent := s.logLines("email sent")
	require.Len(t, sent, 1)
	assert.Equal(t, "verify_email", sent[0]["template"])
	assert.Equal(t, traceA, sent[0]["trace_id"])
	assert.NotContains(t, sent[0], "to", "the recipient address is personal data")
	require.Len(t, ok.sent, 1)
	assert.Contains(t, ok.sent[0].Text, "token=", "the message itself carries the token ...")
	assert.NotContains(t, s.logs.String(), strings.SplitN(strings.SplitN(ok.sent[0].Text, "token=", 2)[1], "\n", 2)[0], "... and the log does not")

	// A permanent failure is an error-level line saying so.
	s.registered("bob@example.com")
	rejecting := &recordingMailer{err: outbox.Permanent(errors.New("recipient rejected"))}
	_, err = s.emailWorker(rejecting).RunOnce(context.Background())
	require.NoError(t, err)
	failed := s.logLines("outbox job failed permanently")
	require.Len(t, failed, 1)
	assert.Equal(t, "error", failed[0]["level"])
	assert.Equal(t, true, failed[0]["permanent"])
	assert.Contains(t, failed[0]["error"], "recipient rejected")
}

func TestObservability_SchedulerRunsGetTheirOwnTraceAndLogTheirOutcome(t *testing.T) {
	s := newStack(t)
	jobs := scheduler.New(scheduler.NewPostgresLocker(s.db.Pool()), s.clock, safego.NewRunner(s.log), s.log)
	jobs.Register(scheduler.Job{Name: "obs.ok", Interval: time.Hour, Run: func(ctx context.Context) (scheduler.Counts, error) {
		zl := s.log.FromContext(ctx)
		zl.Info().Msg("job body running")
		return scheduler.Counts{"issued": 2}, nil
	}})
	jobs.Register(scheduler.Job{Name: "obs.bad", Interval: time.Hour, Run: func(context.Context) (scheduler.Counts, error) {
		return nil, errors.New("gateway unreachable")
	}})

	_, err := jobs.RunOnce(context.Background(), "obs.ok")
	require.NoError(t, err)
	_, err = jobs.RunOnce(context.Background(), "obs.bad")
	require.Error(t, err)

	body := s.logLines("job body running")
	require.Len(t, body, 1)
	assert.Len(t, body[0]["trace_id"], 32, "a run is its own trace")
	assert.NotEmpty(t, body[0]["request_id"])
	finished := s.logLines("job finished")
	require.Len(t, finished, 2)
	byJob := map[any]map[string]any{}
	for _, e := range finished {
		byJob[e["job"]] = e
	}
	assert.Equal(t, body[0]["trace_id"], byJob["obs.ok"]["trace_id"], "the job body and its summary share the trace")
	assert.EqualValues(t, 2, byJob["obs.ok"]["issued"])
	assert.Equal(t, "info", byJob["obs.ok"]["level"])
	assert.Equal(t, "error", byJob["obs.bad"]["level"])
	assert.Contains(t, byJob["obs.bad"]["error"], "gateway unreachable")
}

// midtransBackedFake keeps the fake gateway's name and webhooks (so the stack
// accepts it as its provider) but creates payments through the real Midtrans client.
type midtransBackedFake struct {
	payment.Provider
	real *midtrans.Provider
}

func (m *midtransBackedFake) CreatePayment(ctx context.Context, req payment.PaymentRequest) (payment.PaymentSession, error) {
	return m.real.CreatePayment(ctx, req)
}

// TestObservability_NoSecretEverReachesTheLogs drives every flow that handles a
// secret through the real router, the outbox worker and a Midtrans gateway
// that rejects the call, then checks the captured log output for every secret
// value that was involved.
func TestObservability_NoSecretEverReachesTheLogs(t *testing.T) {
	const midtransKey = "SB-Mid-server-OBSERVABILITYKEY"
	gatewayDown := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error_messages":["Access denied"]}`))
	}))
	t.Cleanup(gatewayDown.Close)
	real, err := midtrans.New(midtrans.Config{ServerKey: midtransKey, SnapBaseURL: gatewayDown.URL, APIBaseURL: gatewayDown.URL}, clock.Real{})
	require.NoError(t, err)
	// The stack's current provider is "fake": route its payment creation to the
	// real Midtrans client, which talks to the failing server above.
	registry, err := payment.NewRegistry(&midtransBackedFake{Provider: fake.New(), real: real})
	require.NoError(t, err)
	s := newStack(t, func(o *app.Options) { o.PaymentProviders = registry })

	secrets := map[string]string{
		"jwt secret":            "0123456789abcdef0123456789abcdef-e2e",
		"midtrans server key":   midtransKey,
		"midtrans basic auth":   base64.StdEncoding.EncodeToString([]byte(midtransKey + ":")),
		"password":              "correct-horse",
		"wrong password":        "wrong-password-guess",
		"new password":          "brand-new-password",
		"reset password":        "reset-new-password",
		"argon2 hash":           "$argon2id$",
		"authorization header":  "Bearer ",
		"fake webhook secret":   "fake-webhook-secret",
		"webhook signature key": fake.Sign([]byte("any")),
	}

	// Registration, verification, login with both delivery modes, refresh, wrong guesses.
	s.registered("ann@example.com")
	s.registered("bob@example.com")
	annToken := s.latestEmailToken("ann@example.com")
	secrets["verification token"] = annToken
	require.Equal(t, http.StatusOK, s.post(verifyPath, `{"token":"`+annToken+`"}`).Code)
	login := s.cookieLogin("ann@example.com")
	secrets["access token"], secrets["refresh cookie"] = login.access, login.refresh
	bodyLogin := s.post(loginPath, `{"email":"ann@example.com","password":"correct-horse","token_delivery":"body"}`)
	require.Equal(t, http.StatusOK, bodyLogin.Code)
	secrets["refresh token in body"] = bodyLogin.json()["refresh_token"].(string)
	require.Equal(t, http.StatusUnauthorized, s.post(loginPath, loginBody("ann@example.com", "wrong-password-guess")).Code)
	_, rotated := s.refreshWithCookie(login.refresh)
	secrets["rotated refresh"] = rotated.refresh
	require.Equal(t, http.StatusUnauthorized, s.post(refreshPath, ``, cookieHeader(login.refresh)).Code, "replay")
	require.Equal(t, http.StatusUnauthorized, s.post(refreshPath, ``, cookieHeader("made-up-cookie-value-123")).Code)
	secrets["made-up refresh"] = "made-up-cookie-value-123"

	// Password change, forgot + reset password, email change with a wrong password first.
	annAccess := s.loggedIn("ann@example.com")
	secrets["access token 2"] = annAccess
	require.Equal(t, http.StatusOK, s.post(changePasswordPath, changePasswordBody("correct-horse", "brand-new-password"), bearer(annAccess)).Code)
	require.Equal(t, http.StatusBadRequest, s.post(changeEmailPath, changeEmailBody("new@example.com", "wrong-password-guess"), bearer(annAccess)).Code)
	require.Equal(t, http.StatusOK, s.post(forgotPath, `{"email":"bob@example.com"}`).Code)
	resetToken := s.latestEmailToken("bob@example.com")
	secrets["reset token"] = resetToken
	require.Equal(t, http.StatusOK, s.post(resetPath, `{"token":"`+resetToken+`","password":"reset-new-password"}`).Code)
	require.Equal(t, http.StatusOK, s.post(changeEmailPath, changeEmailBody("ann.new@example.com", "brand-new-password"), bearer(annAccess)).Code)
	changeToken := s.latestEmailToken("ann.new@example.com")
	secrets["email change token"] = changeToken
	require.Equal(t, http.StatusOK, s.post(confirmEmailPath, `{"token":"`+changeToken+`"}`).Code)

	// Invitation link: the token is in the path of three public routes.
	tm := s.verifiedTeam()
	_, inviteToken := s.invite(tm, "guest@example.com", "member")
	secrets["invitation token"] = inviteToken
	require.Equal(t, http.StatusOK, s.get(invitationLinkPath(inviteToken), nil).Code)
	require.Equal(t, http.StatusNoContent, s.post(invitationLinkPath(inviteToken)+"/decline", ``).Code)

	// Billing: a gateway that rejects the call and echoes our credentials (502),
	// then a signed and an unsigned notification.
	billing := s.billingTeamNamed("biller")
	require.Equal(t, http.StatusBadGateway, s.checkout(billing.owner, billing, "pro").Code)
	number := "INV-2026-000777"
	ev := paid("evt-secret", number, 99000)
	secrets["webhook signature"] = fakeSignatureHeader(ev)[fake.SignatureHeader]
	s.do(http.MethodPost, webhookPath, string(func() []byte { b, _ := fake.Webhook(ev); return b }()), fakeSignatureHeader(ev))
	s.do(http.MethodPost, webhookPath, `{}`, map[string]string{fake.SignatureHeader: "forged-signature-value"})
	secrets["forged signature"] = "forged-signature-value"

	// The worker delivers every email with the real handler.
	mailer := &recordingMailer{}
	worker := s.emailWorker(mailer)
	for {
		n, err := worker.RunOnce(context.Background())
		require.NoError(t, err)
		if n == 0 {
			break
		}
	}
	require.NotEmpty(t, mailer.sent)

	logs := s.logs.String()
	require.NotEmpty(t, logs)
	for name, secret := range secrets {
		assert.NotContains(t, logs, secret, "%s must never be logged", name)
	}
	for _, e := range s.logs.entries(t) { // sanity: the capture is the real thing
		require.NotEmpty(t, e["level"])
		require.NotEmpty(t, e["time"])
	}
}
