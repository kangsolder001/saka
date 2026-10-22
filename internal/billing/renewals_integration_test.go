//go:build integration

package billing_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/billing"
	"github.com/kangsolder001/saka/internal/billing/payment/fake"
	"github.com/kangsolder001/saka/internal/platform/scheduler"
)

func (e *payEnv) renewals() scheduler.Counts {
	e.t.Helper()
	counts, err := e.svc.IssueRenewals(context.Background())
	require.NoError(e.t, err)
	return counts
}

func (e *payEnv) renewalInvoices(orgID uuid.UUID) []invRow {
	e.t.Helper()
	var out []invRow
	for _, inv := range e.invoices(orgID) {
		if inv.Kind == "renewal" {
			out = append(out, inv)
		}
	}
	return out
}

func TestRenewals_IssuesTheNextPeriodsInvoiceThreeDaysBeforeThePeriodEnds(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activePro(orgID, 2*day+time.Hour)
	periodEnd := epoch.Add(2*day + time.Hour)

	counts := e.renewals()

	assert.Equal(t, scheduler.Counts{"issued": 1}, counts)
	invoices := e.renewalInvoices(orgID)
	require.Len(t, invoices, 1)
	inv := invoices[0]
	assert.Equal(t, "INV-2026-000001", inv.Number)
	assert.Equal(t, "pro", inv.Plan)
	assert.Equal(t, int64(99000), inv.Amount)
	assert.Equal(t, "open", inv.Status)
	assert.True(t, periodEnd.Equal(inv.PeriodStart), "the next period starts when this one ends")
	wantEnd, err := billing.AddInterval(periodEnd, "month")
	require.NoError(t, err)
	assert.True(t, wantEnd.Equal(inv.PeriodEnd))
	assert.True(t, periodEnd.Equal(inv.DueAt), "it falls due when the period ends")
	assert.Equal(t, fake.PaymentURL(inv.Number), *inv.PaymentURL)
	require.NotNil(t, inv.Reminded3d, "the invoice itself is the H-3 notice")
	assert.Nil(t, inv.Reminded1d)

	req, err := e.gateway.Last()
	require.NoError(t, err)
	assert.True(t, periodEnd.Add(3*day).Equal(req.ExpiresAt), "the link lives through the grace period")
	emails := e.emails("invoice_created")
	require.Len(t, emails, 1)
	assert.Equal(t, e.ownerEmail(orgID), emails[0].To)
	assert.Equal(t, inv.Number, emails[0].Data["invoice_number"])
}

func TestRenewals_WaitsUntilThePeriodIsInsideTheLeadTime(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activePro(orgID, 5*day)

	assert.Equal(t, scheduler.Counts{}, e.renewals(), "five days out: too early")
	assert.Empty(t, e.invoices(orgID))

	e.clock.Advance(2*day - time.Second) // 3 days and 1 second left
	assert.Empty(t, e.renewalInvoices(orgID))
	assert.Equal(t, scheduler.Counts{}, e.renewals())

	e.clock.Advance(time.Second) // exactly 3 days left
	assert.Equal(t, scheduler.Counts{"issued": 1}, e.renewals())
}

func TestRenewals_RunningAgainCreatesNothing(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activePro(orgID, day)

	first := e.renewals()
	second := e.renewals()
	e.clock.Advance(time.Hour)
	third := e.renewals()

	assert.Equal(t, scheduler.Counts{"issued": 1}, first)
	assert.Equal(t, scheduler.Counts{"exists": 1}, second)
	assert.Equal(t, scheduler.Counts{"exists": 1}, third)
	assert.Len(t, e.renewalInvoices(orgID), 1)
	assert.Len(t, e.gateway.Created(), 1, "one payment at the gateway")
	assert.Len(t, e.emails("invoice_created"), 1, "one email")
}

func TestRenewals_TwoWorkersAtTheSameTimeCreateOneInvoicePerSubscription(t *testing.T) {
	e := newPayEnv(t)
	var orgs []uuid.UUID
	for range 4 {
		orgID := e.newFreeOrg()
		e.activePro(orgID, day)
		orgs = append(orgs, orgID)
	}
	const workers = 5 // more than one, with no scheduler lock between them
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = e.svc.IssueRenewals(context.Background())
		}()
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	for _, orgID := range orgs {
		assert.Len(t, e.renewalInvoices(orgID), 1)
	}
	assert.Len(t, e.gateway.Created(), len(orgs))
	assert.Len(t, e.emails("invoice_created"), len(orgs))
}

func TestRenewals_ABookedDowngradeIsBilledAtTheNewPlan(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	end := epoch.Add(day)
	e.setSub(orgID, subSpec{Plan: "business", Status: billing.StatusActive, PeriodStart: end.AddDate(0, -1, 0), PeriodEnd: &end, PendingPlan: str("pro")})

	e.renewals()

	invoices := e.renewalInvoices(orgID)
	require.Len(t, invoices, 1)
	assert.Equal(t, "pro", invoices[0].Plan)
	assert.Equal(t, int64(99000), invoices[0].Amount)
}

func TestRenewals_AVoidedRenewalIsIssuedAgainWhenTheOrganizationResumes(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activePro(orgID, day)
	e.renewals()
	require.NoError(t, e.svc.CancelSubscription(ownerCtx(orgID)))
	assert.Equal(t, scheduler.Counts{}, e.renewals(), "a subscription set to cancel is not renewed")

	require.NoError(t, e.svc.ResumeSubscription(ownerCtx(orgID)))
	counts := e.renewals()

	assert.Equal(t, scheduler.Counts{"issued": 1}, counts)
	invoices := e.renewalInvoices(orgID)
	require.Len(t, invoices, 2)
	assert.Equal(t, "void", invoices[0].Status)
	assert.Equal(t, "open", invoices[1].Status)
}

func TestRenewals_OnlyRunningPaidSubscriptionsAreRenewed(t *testing.T) {
	e := newPayEnv(t)
	end := epoch.Add(day)
	cases := map[string]subSpec{
		"free":     {Plan: "free", Status: billing.StatusActive, PeriodStart: epoch},
		"trialing": {Plan: "pro", Status: billing.StatusTrialing, TrialEndsAt: &end, PeriodStart: epoch, PeriodEnd: &end},
		"expired":  {Plan: "pro", Status: billing.StatusExpired, PeriodStart: end.AddDate(0, -1, 0), PeriodEnd: &end},
		"canceled": {Plan: "pro", Status: billing.StatusCanceled, PeriodStart: end.AddDate(0, -1, 0), PeriodEnd: &end},
		"cancels":  {Plan: "pro", Status: billing.StatusActive, PeriodStart: end.AddDate(0, -1, 0), PeriodEnd: &end, CancelAtPeriodEnd: true},
	}
	var orgs []uuid.UUID
	for _, spec := range cases {
		orgID := e.newFreeOrg()
		e.setSub(orgID, spec)
		orgs = append(orgs, orgID)
	}

	counts := e.renewals()

	assert.Equal(t, scheduler.Counts{}, counts)
	for _, orgID := range orgs {
		assert.Empty(t, e.invoices(orgID))
	}
}

func TestRenewals_ADeletedOrganizationIsNotBilled(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activePro(orgID, day)
	_, err := e.admin.Exec(context.Background(), "UPDATE organizations SET deleted_at = now() WHERE id = $1", orgID)
	require.NoError(t, err)

	assert.Equal(t, scheduler.Counts{}, e.renewals())
	assert.Empty(t, e.invoices(orgID))
}

func TestRenewals_APastDueSubscriptionStillGetsItsInvoiceWhileThereIsTimeToPay(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	end := epoch.Add(-day) // ended yesterday; the worker was down
	until := end.Add(3 * day)
	e.setSub(orgID, subSpec{Plan: "pro", Status: billing.StatusPastDue, PeriodStart: end.AddDate(0, -1, 0), PeriodEnd: &end, GraceUntil: &until})

	counts := e.renewals()

	assert.Equal(t, scheduler.Counts{"issued": 1}, counts)
	inv := e.renewalInvoices(orgID)[0]
	assert.True(t, end.Equal(inv.PeriodStart))
	require.NotNil(t, inv.Reminded3d, "already past both reminder windows: no reminders to send")
	require.NotNil(t, inv.Reminded1d)
}

func TestRenewals_ATooLateRenewalIsNotIssued(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	end := epoch.Add(-4 * day) // the grace period, and the payment link with it, ended yesterday
	until := end.Add(3 * day)
	e.setSub(orgID, subSpec{Plan: "pro", Status: billing.StatusPastDue, PeriodStart: end.AddDate(0, -1, 0), PeriodEnd: &end, GraceUntil: &until})

	counts := e.renewals()

	assert.Equal(t, scheduler.Counts{"too_late": 1}, counts)
	assert.Empty(t, e.invoices(orgID))
	assert.Empty(t, e.gateway.Created())
}

func TestRenewals_AGatewayFailureIsCountedAndRetriedNextRun(t *testing.T) {
	e := newPayEnv(t)
	good, bad := e.newFreeOrg(), e.newFreeOrg()
	e.activePro(good, day)
	e.activePro(bad, day)
	e.gateway.FailWith(errors.New("gateway down"))

	counts, err := e.svc.IssueRenewals(context.Background())

	require.Error(t, err, "the job reports that something failed")
	assert.Equal(t, scheduler.Counts{"failed": 2}, counts)
	assert.Empty(t, e.invoices(good))
	assert.Empty(t, e.invoices(bad))
	assert.Empty(t, e.emails(""), "no email without an invoice")

	e.gateway.FailWith(nil)
	assert.Equal(t, scheduler.Counts{"issued": 2}, e.renewals())
}

func TestRenewals_OneFailingSubscriptionDoesNotStopTheOthers(t *testing.T) {
	e := newPayEnv(t)
	broken, fine := e.newFreeOrg(), e.newFreeOrg()
	e.activePro(broken, day)
	e.activePro(fine, day)
	// The broken one has an owner whose email cannot be queued.
	_, err := e.admin.Exec(context.Background(), "UPDATE users SET email = 'not-an-address' WHERE id = (SELECT owner_id FROM organizations WHERE id = $1)", broken)
	require.NoError(t, err)

	counts, err := e.svc.IssueRenewals(context.Background())

	require.Error(t, err)
	assert.Equal(t, scheduler.Counts{"issued": 1, "failed": 1}, counts)
	assert.Len(t, e.renewalInvoices(fine), 1)
	assert.Empty(t, e.renewalInvoices(broken), "the failed one rolled back entirely")
}

func TestRenewals_ManySubscriptionsAcrossPages(t *testing.T) {
	e := newPayEnv(t)
	const n = 130 // more than one page of candidates
	for range n {
		e.activePro(e.newFreeOrg(), day)
	}

	counts := e.renewals()

	assert.Equal(t, scheduler.Counts{"issued": n}, counts)
	assert.Equal(t, n, e.countRows("SELECT count(*) FROM invoices WHERE kind = 'renewal'"))
	assert.Equal(t, scheduler.Counts{"exists": n}, e.renewals())
}

func TestRegisterJobs_RegistersTheBillingJobsWithTheirIntervals(t *testing.T) {
	e := newPayEnv(t)
	sched := newTestScheduler(t, e)

	require.NoError(t, e.svc.RegisterJobs(sched))

	res, err := sched.RunOnce(context.Background(), billing.JobRenewals)
	require.NoError(t, err)
	assert.True(t, res.Ran)
}
