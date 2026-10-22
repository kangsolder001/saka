//go:build integration

package billing_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/billing"
	"github.com/kangsolder001/saka/internal/billing/payment"
	"github.com/kangsolder001/saka/internal/platform/scheduler"
)

func (e *payEnv) reconcile() scheduler.Counts {
	e.t.Helper()
	counts, err := e.svc.Reconcile(context.Background())
	require.NoError(e.t, err)
	return counts
}

// The webhook was lost: the customer paid at the gateway, we heard nothing.
// Reconcile asks the gateway and repairs the invoice and the subscription.
func TestReconcile_RepairsAnInvoiceWhoseWebhookWasLost(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	inv := e.buy(orgID, "pro")
	e.gateway.SetState(inv.Number, payment.StatePaid) // paid at the gateway; no notification ever came
	e.clock.Advance(2 * time.Hour)
	require.Equal(t, "open", e.invoiceByNumber(inv.Number).Status)
	require.Equal(t, billing.StatusTrialing, e.sub(orgID).Status)

	counts := e.reconcile()

	assert.Equal(t, scheduler.Counts{"paid": 1}, counts)
	assert.Equal(t, "paid", e.invoiceByNumber(inv.Number).Status)
	sub := e.sub(orgID)
	assert.Equal(t, billing.StatusActive, sub.Status, "the subscription follows, as it would for the webhook")
	assert.True(t, e.clock.Now().Equal(sub.CurrentPeriodStart))
	assert.Len(t, e.emails("payment_succeeded"), 1)
	events := e.events()
	require.Len(t, events, 1)
	assert.Equal(t, "reconcile:"+inv.Number+":paid", events[0].EventID, "recorded like any other event")
	requireProcessedClean(t, events[0])
}

func TestReconcile_RunningAgainOrAfterTheRealWebhookChangesNothing(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	inv := e.buy(orgID, "pro")
	e.gateway.SetState(inv.Number, payment.StatePaid)
	e.clock.Advance(2 * time.Hour)
	e.reconcile()
	afterFirst := e.sub(orgID)
	e.clock.Advance(time.Hour)

	assert.Equal(t, scheduler.Counts{}, e.reconcile(), "a paid invoice is not open any more: nothing to ask")
	require.NoError(t, e.deliver(paidEvent("evt-late", inv.Number, 99000)), "the webhook finally arrives")

	assert.Equal(t, afterFirst, e.sub(orgID))
	assert.Len(t, e.emails("payment_succeeded"), 1, "one confirmation, not two")
}

func TestReconcile_LeavesFreshInvoicesAlone(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	inv := e.buy(orgID, "pro")
	e.gateway.SetState(inv.Number, payment.StatePaid)

	e.clock.Advance(59 * time.Minute)
	assert.Equal(t, scheduler.Counts{}, e.reconcile(), "a customer may still be on the payment page; the webhook is on its way")
	assert.Equal(t, "open", e.invoiceByNumber(inv.Number).Status)

	e.clock.Advance(time.Minute)
	assert.Equal(t, scheduler.Counts{"paid": 1}, e.reconcile())
}

func TestReconcile_PendingAndUnstartedPaymentsAreLeftAlone(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	pending := e.buy(orgID, "pro")
	// An open invoice the gateway has never heard of (the customer never started, or another account).
	_, err := e.admin.Exec(context.Background(), `
		INSERT INTO invoices (organization_id, subscription_id, number, kind, plan_id, amount, currency, status, period_start, period_end, due_at, provider, created_at)
		SELECT $1, s.id, 'INV-2026-000777', 'renewal', s.plan_id, 99000, 'IDR', 'open', $2, $2::timestamptz + interval '1 month', $2, 'fake', $2
		FROM subscriptions s WHERE s.organization_id = $1`, orgID, epoch)
	require.NoError(t, err)
	e.clock.Advance(2 * time.Hour)

	counts := e.reconcile()

	assert.Equal(t, scheduler.Counts{"pending": 1, "not_started": 1}, counts)
	assert.Equal(t, "open", e.invoiceByNumber(pending.Number).Status)
	assert.Empty(t, e.events(), "no event for an answer that decides nothing")
}

func TestReconcile_AnExpiredOrCanceledPaymentClosesTheInvoice(t *testing.T) {
	for state, want := range map[payment.State]string{payment.StateExpired: "expired", payment.StateCanceled: "void"} {
		t.Run(string(state), func(t *testing.T) {
			e := newPayEnv(t)
			orgID := e.newFreeOrg()
			inv := e.buy(orgID, "pro")
			e.gateway.SetState(inv.Number, state)
			e.clock.Advance(2 * time.Hour)

			counts := e.reconcile()

			assert.Equal(t, scheduler.Counts{string(state): 1}, counts)
			assert.Equal(t, want, e.invoiceByNumber(inv.Number).Status)
			assert.Equal(t, billing.StatusTrialing, e.sub(orgID).Status)
		})
	}
}

func TestReconcile_AFailedPaymentStaysOpenForAnotherTry(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	inv := e.buy(orgID, "pro")
	e.gateway.SetState(inv.Number, payment.StateFailed)
	e.clock.Advance(2 * time.Hour)

	assert.Equal(t, scheduler.Counts{"pending": 1}, e.reconcile())
	assert.Equal(t, "open", e.invoiceByNumber(inv.Number).Status)
}

func TestReconcile_TheGatewaysAnswerIsCheckedLikeAWebhook(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	inv := e.buy(orgID, "pro")
	e.gateway.SetState(inv.Number, payment.StatePaid)
	_, err := e.admin.Exec(context.Background(), "UPDATE invoices SET amount = 150000 WHERE number = $1", inv.Number) // the gateway says 99000
	require.NoError(t, err)
	e.clock.Advance(2 * time.Hour)

	e.reconcile()

	assert.Equal(t, "open", e.invoiceByNumber(inv.Number).Status, "an amount that is not the invoice's activates nothing")
	assert.Equal(t, billing.StatusTrialing, e.sub(orgID).Status)
	requireFlagged(t, e.events()[0], "amount mismatch")
}

func TestReconcile_AGatewayFailureIsCountedAndTheOtherInvoicesAreStillChecked(t *testing.T) {
	e := newPayEnv(t)
	a, b := e.newFreeOrg(), e.newFreeOrg()
	invA, invB := e.buy(a, "pro"), e.buy(b, "pro")
	e.gateway.SetState(invA.Number, payment.StatePaid)
	e.gateway.SetState(invB.Number, payment.StatePaid)
	e.clock.Advance(2 * time.Hour)
	e.gateway.FailWith(errors.New("gateway down"))

	counts, err := e.svc.Reconcile(context.Background())

	require.Error(t, err)
	assert.Equal(t, scheduler.Counts{"failed": 2}, counts)
	assert.Equal(t, "open", e.invoiceByNumber(invA.Number).Status)

	e.gateway.FailWith(nil)
	assert.Equal(t, scheduler.Counts{"paid": 2}, e.reconcile(), "and the next run repairs both")
}

func TestReconcile_TwoWorkersAtTheSameTimeRepairEachInvoiceOnce(t *testing.T) {
	e := newPayEnv(t)
	var numbers []string
	for range 4 {
		inv := e.buy(e.newFreeOrg(), "pro")
		e.gateway.SetState(inv.Number, payment.StatePaid)
		numbers = append(numbers, inv.Number)
	}
	e.clock.Advance(2 * time.Hour)
	const workers = 5
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = e.svc.Reconcile(context.Background())
		}()
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	for _, number := range numbers {
		assert.Equal(t, "paid", e.invoiceByNumber(number).Status)
	}
	assert.Len(t, e.emails("payment_succeeded"), 4, "one confirmation per payment")
	assert.Len(t, e.events(), 4, "one event per payment")
	for _, ev := range e.events() {
		requireProcessedClean(t, ev)
	}
}

func TestReconcile_AnInvoiceOfAnotherProviderIsLeftAlone(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	inv := e.buy(orgID, "pro")
	_, err := e.admin.Exec(context.Background(), "UPDATE invoices SET provider = 'midtrans' WHERE number = $1", inv.Number)
	require.NoError(t, err)
	e.gateway.SetState(inv.Number, payment.StatePaid)
	e.clock.Advance(2 * time.Hour)

	assert.Equal(t, scheduler.Counts{"unknown_provider": 1}, e.reconcile())
	assert.Equal(t, "open", e.invoiceByNumber(inv.Number).Status)
}

func TestRegisterJobs_RegistersAllFiveJobsOnTheirIntervals(t *testing.T) {
	e := newPayEnv(t)
	sched := newTestScheduler(t, e)
	require.NoError(t, e.svc.RegisterJobs(sched))

	ran := sched.RunDue(context.Background())

	assert.Equal(t, []string{billing.JobRenewals, billing.JobApplyScheduled, billing.JobExpire, billing.JobReminders, billing.JobReconcile}, ran)
	e.clock.Advance(time.Hour)
	assert.Equal(t, []string{billing.JobRenewals, billing.JobApplyScheduled, billing.JobExpire}, sched.RunDue(context.Background()), "the daily ones wait for the day")
	e.clock.Advance(23 * time.Hour)
	assert.Len(t, sched.RunDue(context.Background()), 5)
}
