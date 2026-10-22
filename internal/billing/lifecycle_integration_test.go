//go:build integration

package billing_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/billing"
	"github.com/kangsolder001/saka/internal/platform/scheduler"
)

// runAllJobs runs the whole schedule once, in the order the worker registers it.
func (e *payEnv) runAllJobs() {
	e.t.Helper()
	ctx := context.Background()
	jobs := []struct {
		name string
		run  func(context.Context) (scheduler.Counts, error)
	}{
		{billing.JobRenewals, e.svc.IssueRenewals},
		{billing.JobApplyScheduled, e.svc.ApplyScheduled},
		{billing.JobExpire, e.svc.ExpireSubscriptions},
		{billing.JobReminders, e.svc.SendReminders},
		{billing.JobReconcile, e.svc.Reconcile},
	}
	for _, job := range jobs {
		_, err := job.run(ctx)
		require.NoError(e.t, err, job.name)
	}
}

// at moves the fake clock to epoch + d and runs the schedule, as the worker
// would have by then.
func (e *payEnv) at(d time.Duration) {
	e.t.Helper()
	e.clock.Set(epoch.Add(d))
	e.runAllJobs()
}

// pay delivers the gateway's confirmation of a whole invoice.
func (e *payEnv) pay(number string, eventID string) {
	e.t.Helper()
	inv := e.invoiceByNumber(number)
	require.NoError(e.t, e.deliver(paidEvent(eventID, number, inv.Amount)))
}

func (e *payEnv) requireState(orgID uuid.UUID, status billing.Status, plan, effective string) subRow {
	e.t.Helper()
	sub := e.sub(orgID)
	assert.Equal(e.t, status, sub.Status, "status at %s", e.clock.Now().Format(time.RFC3339))
	assert.Equal(e.t, plan, sub.Plan, "plan at %s", e.clock.Now().Format(time.RFC3339))
	assert.Equal(e.t, effective, e.effectivePlan(orgID), "the plan in force at %s", e.clock.Now().Format(time.RFC3339))
	return sub
}

// Every transition of spec §8.2 happens here, in the order a real organization
// meets them, driven only by the fake clock, the webhook and the jobs.
func TestLifecycle_FreeTrialPaidRenewedCanceledAndBack(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.requireState(orgID, billing.StatusActive, "free", "free") // registration: Free

	// free -> trialing: the owner picks Pro and gets the trial.
	res, err := e.svc.Checkout(ownerCtx(orgID), "pro")
	require.NoError(t, err)
	require.True(t, res.TrialStarted)
	e.requireState(orgID, billing.StatusTrialing, "pro", "pro")

	// trialing -> active: paid on day 5; the period starts then.
	e.at(5 * day)
	e.requireState(orgID, billing.StatusTrialing, "pro", "pro")
	e.pay(res.Invoice.Number, "evt-1")
	sub := e.requireState(orgID, billing.StatusActive, "pro", "pro")
	paidAt := epoch.Add(5 * day)
	assert.True(t, paidAt.Equal(sub.CurrentPeriodStart))
	firstEnd, err := billing.AddInterval(paidAt, "month")
	require.NoError(t, err)
	assert.True(t, firstEnd.Equal(*sub.CurrentPeriodEnd))

	// active -> active: the renewal is issued three days ahead, paid, and the
	// next period begins exactly when the old one ends.
	e.clock.Set(firstEnd.Add(-3*day - time.Hour))
	e.runAllJobs()
	assert.Empty(t, e.renewalInvoices(orgID), "not yet")
	e.clock.Set(firstEnd.Add(-3 * day))
	e.runAllJobs()
	renewals := e.renewalInvoices(orgID)
	require.Len(t, renewals, 1)
	e.pay(renewals[0].Number, "evt-2")
	e.requireState(orgID, billing.StatusActive, "pro", "pro")
	e.clock.Set(firstEnd.Add(-time.Second))
	e.runAllJobs()
	assert.True(t, paidAt.Equal(e.sub(orgID).CurrentPeriodStart), "still the first period")
	e.clock.Set(firstEnd)
	e.runAllJobs()
	sub = e.requireState(orgID, billing.StatusActive, "pro", "pro")
	assert.True(t, firstEnd.Equal(sub.CurrentPeriodStart))
	secondEnd, err := billing.AddInterval(firstEnd, "month")
	require.NoError(t, err)
	assert.True(t, secondEnd.Equal(*sub.CurrentPeriodEnd))

	// active -> canceled: the owner cancels; access lasts until the period ends.
	require.NoError(t, e.svc.CancelSubscription(ownerCtx(orgID)))
	e.clock.Set(secondEnd.Add(-time.Second))
	e.runAllJobs()
	e.requireState(orgID, billing.StatusActive, "pro", "pro")
	e.clock.Set(secondEnd)
	e.runAllJobs()
	e.requireState(orgID, billing.StatusCanceled, "pro", "free")
	assert.Empty(t, e.emails("subscription_past_due"), "nobody chases a canceled subscription")

	// canceled -> active: subscribing again, with no second trial.
	again, err := e.svc.Checkout(ownerCtx(orgID), "business")
	require.NoError(t, err)
	assert.False(t, again.TrialStarted)
	e.requireState(orgID, billing.StatusCanceled, "pro", "free")
	e.pay(again.Invoice.Number, "evt-3")
	e.requireState(orgID, billing.StatusActive, "business", "business")
}

func TestLifecycle_AnUnpaidTrialExpiresAndTheOrganizationSubscribesAgain(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	res, err := e.svc.Checkout(ownerCtx(orgID), "pro")
	require.NoError(t, err)

	// trialing -> expired
	e.at(11 * day)
	e.requireState(orgID, billing.StatusTrialing, "pro", "pro")
	assert.Len(t, e.emails("trial_ending"), 1, "told three days before")
	e.at(14*day - time.Second)
	e.requireState(orgID, billing.StatusTrialing, "pro", "pro")
	e.at(14 * day)
	e.requireState(orgID, billing.StatusExpired, "pro", "free")
	assert.Len(t, e.emails("subscription_expired"), 1)
	assert.Equal(t, "expired", e.invoiceByNumber(res.Invoice.Number).Status)

	// expired -> active
	again, err := e.svc.Checkout(ownerCtx(orgID), "pro")
	require.NoError(t, err)
	assert.False(t, again.TrialStarted, "a trial is only given once")
	e.at(15 * day)
	e.pay(again.Invoice.Number, "evt-1")
	sub := e.requireState(orgID, billing.StatusActive, "pro", "pro")
	assert.True(t, epoch.Add(15*day).Equal(sub.CurrentPeriodStart))
}

func TestLifecycle_AnUnpaidRenewalGoesPastDueAndEitherPaysWithinTheGraceOrExpires(t *testing.T) {
	for _, tc := range []struct {
		name string
		pays bool
	}{{"pays within the grace period", true}, {"does not pay", false}} {
		t.Run(tc.name, func(t *testing.T) {
			e := newPayEnv(t)
			orgID := e.newFreeOrg()
			e.activePro(orgID, 10*day)
			end := epoch.Add(10 * day)

			e.at(7 * day) // three days before the end
			renewal := e.renewalInvoices(orgID)[0]
			assert.Len(t, e.emails("invoice_created"), 1)

			// active -> past_due
			e.at(10 * day)
			sub := e.requireState(orgID, billing.StatusPastDue, "pro", "pro")
			assert.True(t, end.Add(3*day).Equal(*sub.GraceUntil))
			assert.Len(t, e.emails("subscription_past_due"), 1)

			e.at(12 * day)
			e.requireState(orgID, billing.StatusPastDue, "pro", "pro")

			if tc.pays {
				// past_due -> active: paid in time; the next period follows the old one.
				e.pay(renewal.Number, "evt-1")
				sub = e.requireState(orgID, billing.StatusActive, "pro", "pro")
				assert.True(t, end.Equal(sub.CurrentPeriodStart))
				assert.Nil(t, sub.GraceUntil)
				e.at(20 * day)
				e.requireState(orgID, billing.StatusActive, "pro", "pro")
				return
			}
			// past_due -> expired
			e.at(13*day - time.Second)
			e.requireState(orgID, billing.StatusPastDue, "pro", "pro")
			e.at(13 * day)
			e.requireState(orgID, billing.StatusExpired, "pro", "free")
			assert.Equal(t, "expired", e.invoiceByNumber(renewal.Number).Status)
			assert.Len(t, e.emails("subscription_expired"), 1)
		})
	}
}

func TestLifecycle_ADowngradeBookedMidPeriodAppliesAtTheEndAndBillsTheSmallerPlan(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.setSub(orgID, subSpec{Plan: "business", Status: billing.StatusActive, PeriodStart: epoch.AddDate(0, -1, 0), PeriodEnd: at30(epoch)})
	_, err := e.svc.ChangePlan(ownerCtx(orgID), "pro")
	require.NoError(t, err)
	e.requireState(orgID, billing.StatusActive, "business", "business")

	e.at(27 * day)
	renewal := e.renewalInvoices(orgID)[0]
	assert.Equal(t, "pro", renewal.Plan)
	assert.Equal(t, int64(99000), renewal.Amount, "Pro's price, not Business's")
	e.pay(renewal.Number, "evt-1")
	e.requireState(orgID, billing.StatusActive, "business", "business")

	e.at(30 * day)
	sub := e.requireState(orgID, billing.StatusActive, "pro", "pro")
	assert.Nil(t, sub.PendingPlan)
}

func at30(from time.Time) *time.Time { t := from.Add(30 * day); return &t }

// Two worker processes, each with its own scheduler on the same database,
// running the whole schedule at the same moments, leave exactly what one would.
func TestWorkers_TwoSchedulersRunningTheWholeScheduleAtOnceLeaveWhatOneWouldHaveLeft(t *testing.T) {
	e := newPayEnv(t)
	end := epoch.Add(2 * day)
	var renewing, trialing, lapsing, canceling []uuid.UUID
	for range 3 {
		a := e.newFreeOrg()
		e.activePro(a, 2*day)
		renewing = append(renewing, a)
		b := e.newFreeOrg()
		e.setSub(b, subSpec{Plan: "pro", Status: billing.StatusTrialing, TrialEndsAt: &end, PeriodStart: epoch.AddDate(0, 0, -12), PeriodEnd: &end})
		trialing = append(trialing, b)
		c := e.newFreeOrg()
		e.activePro(c, day)
		lapsing = append(lapsing, c)
		d := e.newFreeOrg()
		e.activePro(d, 2*day)
		canceling = append(canceling, d)
	}
	// Renewals exist for the first and fourth groups; the first group paid.
	e.runAllJobs()
	for _, orgID := range renewing {
		e.pay(e.renewalInvoices(orgID)[0].Number, "paid-"+orgID.String())
	}
	for _, orgID := range canceling {
		require.NoError(t, e.svc.CancelSubscription(ownerCtx(orgID)))
	}
	e.clock.Set(epoch.Add(2*day + time.Minute)) // everything is due

	workers := make([]*scheduler.Scheduler, 2)
	for i := range workers {
		workers[i] = newTestScheduler(t, e)
		require.NoError(t, e.svc.RegisterJobs(workers[i]))
	}
	var wg sync.WaitGroup
	for _, w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.RunDue(context.Background())
		}()
	}
	wg.Wait()
	// A row one worker had locked while the other looked is left for the next
	// run, an hour on (SKIP LOCKED is not a wait); by then everything is done.
	e.clock.Advance(time.Hour)
	workers[0].RunDue(context.Background())

	for _, orgID := range renewing {
		e.requireState(orgID, billing.StatusActive, "pro", "pro")
		assert.True(t, end.Equal(e.sub(orgID).CurrentPeriodStart), "rolled into the paid period once")
	}
	for _, orgID := range trialing {
		e.requireState(orgID, billing.StatusExpired, "pro", "free")
	}
	for _, orgID := range lapsing {
		e.requireState(orgID, billing.StatusPastDue, "pro", "pro")
	}
	for _, orgID := range canceling {
		e.requireState(orgID, billing.StatusCanceled, "pro", "free")
	}
	assert.Len(t, e.emails("subscription_expired"), 3)
	assert.Len(t, e.emails("subscription_past_due"), 3)
	assert.Len(t, e.emails("payment_succeeded"), 3)
	// Renewals exist for the paying, the lapsing and the canceling group (the last voided when they canceled): 3 x 3.
	assert.Equal(t, 9, e.countRows("SELECT count(*) FROM invoices WHERE kind = 'renewal'"))
	assert.Equal(t, 0, e.countRows("SELECT count(*) FROM (SELECT subscription_id, period_start FROM invoices WHERE kind = 'renewal' AND status <> 'void' GROUP BY 1, 2 HAVING count(*) > 1) d"),
		"never two renewals for one period")
}
