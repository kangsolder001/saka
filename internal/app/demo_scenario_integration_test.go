//go:build integration

package app_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/billing"
	"github.com/kangsolder001/saka/internal/billing/payment"
	"github.com/kangsolder001/saka/internal/mail"
	"github.com/kangsolder001/saka/internal/org"
	"github.com/kangsolder001/saka/internal/platform/config"
	"github.com/kangsolder001/saka/internal/platform/outbox"
	"github.com/kangsolder001/saka/internal/platform/scheduler"
	emailtemplates "github.com/kangsolder001/saka/templates/email"
)

// billingWorker is the billing side of the worker process (cmd/worker), built on the
// stack's database, clock and fake gateway: its jobs are what move a
// subscription forward when nobody calls the API.
type billingWorker struct {
	t   *testing.T
	svc *billing.Service
}

func (s *stack) billingWorker() *billingWorker {
	s.t.Helper()
	registry, err := payment.NewRegistry(s.gateway)
	require.NoError(s.t, err)
	renderer, err := mail.NewRenderer(emailtemplates.FS, mail.AppInfo{Name: "Saka"})
	require.NoError(s.t, err)
	svc := billing.NewService(s.clock)
	svc.EnablePayments(billing.PaymentDependencies{
		Tx:        s.db,
		Mail:      mail.NewService(renderer, outbox.NewStore(s.db.Pool(), s.clock)),
		Contacts:  org.NewService(org.Dependencies{Clock: s.clock, Tx: s.db, Users: s.users}),
		Providers: registry,
		Settings:  billing.SettingsFrom(config.Default().Billing),
		Log:       s.log,
	})
	return &billingWorker{t: s.t, svc: svc}
}

// runDueJobs runs the whole schedule once, in the order the worker registers it.
func (w *billingWorker) runDueJobs() {
	w.t.Helper()
	for _, job := range []struct {
		name string
		run  func(context.Context) (scheduler.Counts, error)
	}{
		{billing.JobRenewals, w.svc.IssueRenewals},
		{billing.JobApplyScheduled, w.svc.ApplyScheduled},
		{billing.JobExpire, w.svc.ExpireSubscriptions},
		{billing.JobReminders, w.svc.SendReminders},
	} {
		_, err := job.run(context.Background())
		require.NoError(w.t, err, job.name)
	}
}

// seatsOf reads the seats used and allowed, the plan in force and the status from the subscription endpoint.
func (s *stack) seatsOf(tm team, access string) (used, max float64, plan, status string) {
	s.t.Helper()
	sub := subscriptionOf(s.get(tm.subscriptionPath(), bearer(access)))
	seats := sub["usage"].(map[string]any)["members"].(map[string]any)
	return seats["used"].(float64), seats["max"].(float64), sub["effective_plan"].(string), sub["status"].(string)
}

func (s *stack) memberCount(tm team, access string) int {
	s.t.Helper()
	res := s.get(orgPath(tm.orgID)+"/members", bearer(access))
	require.Equal(s.t, http.StatusOK, res.Code, res.Body.String())
	return len(res.json()["items"].([]any))
}

// TestDemoScenario_RegisterInviteHitTheLimitUpgradeThenLapseBackToFree is the
// story docs/demo.md tells with curl, as one test: a team signs up, grows to
// the Free limit, upgrades, forgets to renew, and lands back on Free with
// everything it created still there. Only the fake clock and the fake gateway
// are artificial; every step goes through the real router and the real jobs.
func TestDemoScenario_RegisterInviteHitTheLimitUpgradeThenLapseBackToFree(t *testing.T) {
	s := newStack(t)
	worker := s.billingWorker()

	// Day 0: Ann signs up. She gets her organization on the Free plan, with
	// three seats, and has to prove her email before she can invite anyone.
	s.registered("ann@example.com")
	ann := s.loggedIn("ann@example.com")
	tm := team{orgID: s.firstOrg("ann@example.com"), owner: ann}
	used, limit, plan, status := s.seatsOf(tm, ann)
	assert.Equal(t, []any{1.0, 3.0, "free", "active"}, []any{used, limit, plan, status}, "a new organization: Free, one seat of three used")

	refused := s.post(tm.invitationsPath(), inviteBody("bob@example.com", "member"), bearer(ann))
	require.Equal(t, http.StatusForbidden, refused.Code)
	assert.Equal(t, "EMAIL_NOT_VERIFIED", errorOf(refused)["code"])

	s.verifyByLink("ann@example.com")
	ann = s.loggedIn("ann@example.com") // a fresh token; the cached profile already shows the verified email
	tm.owner = ann

	// She invites Bob, who has no account yet: he registers and proves his
	// address, which claims the invitation. And Carol, who already has one and
	// accepts from the link.
	_, bobToken := s.invite(tm, "bob@example.com", "member")
	info := s.get(invitationLinkPath(bobToken), nil)
	require.Equal(t, http.StatusOK, info.Code)
	assert.Equal(t, false, info.json()["invitation"].(map[string]any)["account_exists"], "the page sends Bob to sign up")
	s.registered("bob@example.com")
	s.verifyByLink("bob@example.com")

	s.registered("carol@example.com")
	s.verifyByLink("carol@example.com")
	_, carolToken := s.invite(tm, "carol@example.com", "member")
	carol := s.loggedIn("carol@example.com")
	accepted := s.post(invitationLinkPath(carolToken)+"/accept", ``, bearer(carol))
	require.Equal(t, http.StatusOK, accepted.Code, accepted.Body.String())
	assert.Equal(t, 3, s.memberCount(tm, ann), "Ann, Bob and Carol")

	// The Free plan is full: the fourth person is refused with a way forward.
	used, limit, plan, _ = s.seatsOf(tm, ann)
	assert.Equal(t, []any{3.0, 3.0, "free"}, []any{used, limit, plan})
	full := s.post(tm.invitationsPath(), inviteBody("dave@example.com", "member"), bearer(ann))
	require.Equal(t, http.StatusPaymentRequired, full.Code, full.Body.String())
	assert.Equal(t, "PLAN_LIMIT_REACHED", errorOf(full)["code"])
	assert.Equal(t, map[string]any{"limit": "members", "max": 3.0, "used": 3.0, "plan": "free", "upgrade_to": "pro"}, detailsOf(full))

	// Upgrade: Ann picks Pro. The first time brings the 14-day trial, so Pro's
	// ten seats are available at once, and the invoice carries the payment link.
	checkout := s.checkout(ann, tm, "pro")
	require.Equal(t, http.StatusCreated, checkout.Code, checkout.Body.String())
	invoice := invoiceOf(checkout)
	number := invoice["number"].(string)
	assert.Equal(t, true, checkout.json()["trial_started"])
	assert.NotEmpty(t, invoice["payment_url"], "the customer pays at the gateway")
	_, limit, plan, status = s.seatsOf(tm, ann)
	assert.Equal(t, []any{10.0, "pro", "trialing"}, []any{limit, plan, status})
	_, _ = s.invite(tm, "dave@example.com", "member") // now it fits
	s.registered("dave@example.com")
	s.verifyByLink("dave@example.com")
	assert.Equal(t, 4, s.memberCount(tm, ann), "Dave joined: more members than Free allows")

	// The next day the gateway confirms the payment; only a verified webhook
	// activates the plan, and the period starts when the money arrived.
	s.clock.Advance(24 * time.Hour)
	require.Equal(t, http.StatusOK, s.paymentWebhook(paid("evt-demo-1", number, 99000)).Code)
	ann = s.loggedIn("ann@example.com") // access tokens last 15 minutes: a day later she logs in again
	sub := subscriptionOf(s.get(tm.subscriptionPath(), bearer(ann)))
	assert.Equal(t, "active", sub["status"])
	assert.Equal(t, "2026-06-02T10:00:00Z", sub["current_period_start"])
	assert.Equal(t, "2026-07-02T10:00:00Z", sub["current_period_end"])
	assert.Equal(t, 1, s.emailsTo("ann@example.com", "payment_succeeded"))

	// Four weeks on: three days before the period ends the worker issues the
	// renewal invoice and emails it. Ann does not pay it.
	s.clock.Set(time.Date(2026, 6, 29, 11, 0, 0, 0, time.UTC))
	worker.runDueJobs()
	ann = s.loggedIn("ann@example.com")
	invoices := s.get(tm.invoicesPath(), bearer(ann)).json()["items"].([]any)
	require.Len(t, invoices, 2, "the paid checkout invoice and the renewal")
	renewal := invoices[0].(map[string]any)
	assert.Equal(t, "renewal", renewal["kind"])
	assert.Equal(t, "open", renewal["status"])
	assert.Equal(t, 2, s.emailsTo("ann@example.com", "invoice_created"), "the checkout invoice and the renewal")
	_, _, _, status = s.seatsOf(tm, ann)
	assert.Equal(t, "active", status, "still inside the paid period")

	// The period ends unpaid: past_due, with three days of grace in which
	// nothing changes for the team.
	s.clock.Set(time.Date(2026, 7, 2, 11, 0, 0, 0, time.UTC))
	worker.runDueJobs()
	ann = s.loggedIn("ann@example.com")
	sub = subscriptionOf(s.get(tm.subscriptionPath(), bearer(ann)))
	assert.Equal(t, "past_due", sub["status"])
	assert.Equal(t, "2026-07-05T10:00:00Z", sub["grace_until"])
	assert.Equal(t, "pro", sub["effective_plan"], "the grace period keeps the paid plan")
	assert.Equal(t, 1, s.emailsTo("ann@example.com", "subscription_past_due"))

	// The grace ends: expired. The team is back on Free's limits...
	s.clock.Set(time.Date(2026, 7, 5, 11, 0, 0, 0, time.UTC))
	worker.runDueJobs()
	ann = s.loggedIn("ann@example.com")
	used, limit, plan, status = s.seatsOf(tm, ann)
	assert.Equal(t, []any{4.0, 3.0, "free", "expired"}, []any{used, limit, plan, status}, "four seats used of Free's three")
	assert.Equal(t, 1, s.emailsTo("ann@example.com", "subscription_expired"))
	capped := s.post(tm.invitationsPath(), inviteBody("erin@example.com", "member"), bearer(ann))
	require.Equal(t, http.StatusPaymentRequired, capped.Code)
	assert.Equal(t, "free", detailsOf(capped)["plan"])
	renewalStatus := s.get(tm.invoicesPath(), bearer(ann)).json()["items"].([]any)[0].(map[string]any)["status"]
	assert.Equal(t, "expired", renewalStatus, "the unpaid renewal can no longer be paid")

	// ...but nothing was taken away: the organization, its members and their
	// access are all still there. Only new seats are blocked until Ann pays again.
	assert.Equal(t, 4, s.memberCount(tm, ann), "no member was removed")
	assert.Equal(t, http.StatusOK, s.get(orgPath(tm.orgID), bearer(s.loggedIn("dave@example.com"))).Code, "Dave still gets in")
	assert.Equal(t, http.StatusOK, s.get(tm.invoicesPath(), bearer(ann)).Code, "and Ann can still reach her billing history")

	// Coming back is one checkout away (no second trial), after which the
	// seats are available again.
	again := s.checkout(ann, tm, "pro")
	require.Equal(t, http.StatusCreated, again.Code, again.Body.String())
	assert.Equal(t, false, again.json()["trial_started"], "the trial is used once per organization")
	require.Equal(t, http.StatusOK, s.paymentWebhook(paid("evt-demo-2", invoiceOf(again)["number"].(string), 99000)).Code)
	_, limit, plan, status = s.seatsOf(tm, ann)
	assert.Equal(t, []any{10.0, "pro", "active"}, []any{limit, plan, status})
	assert.Equal(t, http.StatusCreated, s.post(tm.invitationsPath(), inviteBody("erin@example.com", "member"), bearer(ann)).Code)
}
