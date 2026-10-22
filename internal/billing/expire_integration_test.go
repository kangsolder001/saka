//go:build integration

package billing_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/billing"
	"github.com/kangsolder001/saka/internal/billing/payment/fake"
	"github.com/kangsolder001/saka/internal/platform/scheduler"
)

func (e *payEnv) expire() scheduler.Counts {
	e.t.Helper()
	counts, err := e.svc.ExpireSubscriptions(context.Background())
	require.NoError(e.t, err)
	return counts
}

func (e *payEnv) applyScheduled() scheduler.Counts {
	e.t.Helper()
	counts, err := e.svc.ApplyScheduled(context.Background())
	require.NoError(e.t, err)
	return counts
}

// effectivePlan is the plan the organization may use right now.
func (e *payEnv) effectivePlan(orgID uuid.UUID) string {
	e.t.Helper()
	var code string
	e.inTenant(orgID, func(ctx context.Context, tx pgx.Tx) {
		p, err := e.svc.EffectivePlan(ctx, tx, orgID)
		require.NoError(e.t, err)
		code = p.Code
	})
	return code
}

// --- trial ---

func TestExpire_AnUnpaidTrialExpiresWhenItEnds(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	checkout := e.buy(orgID, "pro")
	trialEnd := epoch.AddDate(0, 0, 14)

	e.clock.Set(trialEnd.Add(-time.Second))
	assert.Equal(t, scheduler.Counts{}, e.expire(), "one second before the end: still a trial")
	assert.Equal(t, billing.StatusTrialing, e.sub(orgID).Status)
	assert.Equal(t, "pro", e.effectivePlan(orgID))

	e.clock.Set(trialEnd)
	counts := e.expire()

	assert.Equal(t, 1, counts["expired"])
	assert.Equal(t, billing.StatusExpired, e.sub(orgID).Status)
	assert.Equal(t, "free", e.effectivePlan(orgID), "the organization is back on the Free limits")
	assert.Equal(t, "expired", e.invoiceByNumber(checkout.Number).Status, "the trial's invoice cannot be paid any more")
	emails := e.emails("subscription_expired")
	require.Len(t, emails, 1)
	assert.Equal(t, e.ownerEmail(orgID), emails[0].To)
	assert.Equal(t, "Pro", emails[0].Data["plan_name"])
}

func TestExpire_APaidTrialIsNotExpired(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	inv := e.buy(orgID, "pro")
	require.NoError(t, e.deliver(paidEvent("evt-1", inv.Number, 99000)))
	e.clock.Set(epoch.AddDate(0, 0, 15))

	assert.Equal(t, scheduler.Counts{}, e.expire())
	assert.Equal(t, billing.StatusActive, e.sub(orgID).Status)
}

// --- period end, grace ---

func TestExpire_AnUnpaidPeriodEndStartsTheGracePeriodAndWarnsTheOwner(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activePro(orgID, 2*day)
	periodEnd := epoch.Add(2 * day)
	e.renewals()
	renewal := e.renewalInvoices(orgID)[0]

	e.clock.Set(periodEnd.Add(-time.Second))
	assert.Equal(t, scheduler.Counts{}, e.expire())

	e.clock.Set(periodEnd.Add(20 * time.Minute)) // the job runs a little late
	counts := e.expire()

	assert.Equal(t, 1, counts["past_due"])
	sub := e.sub(orgID)
	assert.Equal(t, billing.StatusPastDue, sub.Status)
	require.NotNil(t, sub.GraceUntil)
	assert.True(t, periodEnd.Add(3*day).Equal(*sub.GraceUntil), "the grace period counts from the period end, not from when the job ran")
	assert.Equal(t, "pro", e.effectivePlan(orgID), "access continues during the grace period")
	assert.Equal(t, "open", e.invoiceByNumber(renewal.Number).Status, "and the renewal can still be paid")
	emails := e.emails("subscription_past_due")
	require.Len(t, emails, 1)
	assert.Equal(t, fake.PaymentURL(renewal.Number), emails[0].Data["payment_url"], "with a way to pay")
	assert.Equal(t, billing.FormatDate(periodEnd.Add(3*day)), emails[0].Data["grace_until"])

	assert.Equal(t, scheduler.Counts{}, e.expire(), "running again changes nothing: nothing is due until the grace period ends")
	assert.Len(t, e.emails("subscription_past_due"), 1)
}

func TestExpire_PastDueWithoutARenewalInvoiceStillWarnsWithoutALink(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activePro(orgID, day)
	e.clock.Set(epoch.Add(day))

	e.expire()

	emails := e.emails("subscription_past_due")
	require.Len(t, emails, 1)
	assert.Equal(t, "", emails[0].Data["payment_url"])
}

func TestExpire_TheGracePeriodEndsInExpiry(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activePro(orgID, 2*day)
	periodEnd := epoch.Add(2 * day)
	e.renewals()
	renewal := e.renewalInvoices(orgID)[0]
	e.clock.Set(periodEnd)
	e.expire()
	require.Equal(t, billing.StatusPastDue, e.sub(orgID).Status)

	e.clock.Set(periodEnd.Add(3*day - time.Second))
	assert.Equal(t, scheduler.Counts{}, e.expire())
	assert.Equal(t, "pro", e.effectivePlan(orgID))

	e.clock.Set(periodEnd.Add(3 * day))
	counts := e.expire()

	assert.Equal(t, 1, counts["expired"])
	assert.Equal(t, billing.StatusExpired, e.sub(orgID).Status)
	assert.Equal(t, "free", e.effectivePlan(orgID))
	assert.Equal(t, "expired", e.invoiceByNumber(renewal.Number).Status)
	assert.Len(t, e.emails("subscription_expired"), 1)
	assert.Equal(t, scheduler.Counts{}, e.expire(), "and then nothing more to do")
}

func TestExpire_APastDueSubscriptionThatPaysIsNotExpired(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activePro(orgID, 2*day)
	periodEnd := epoch.Add(2 * day)
	e.renewals()
	renewal := e.renewalInvoices(orgID)[0]
	e.clock.Set(periodEnd)
	e.expire()

	e.clock.Set(periodEnd.Add(2 * day))
	require.NoError(t, e.deliver(paidEvent("evt-1", renewal.Number, 99000)))
	e.clock.Set(periodEnd.Add(10 * day))

	assert.Equal(t, scheduler.Counts{}, e.expire())
	assert.Equal(t, billing.StatusActive, e.sub(orgID).Status)
}

func TestExpire_AWorkerThatWasDownCatchesUpOneStepPerRunAndTheEndOfTheGraceIsExactAnyway(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activePro(orgID, day)
	e.clock.Set(epoch.Add(day + 5*day)) // five days after the period end: the grace period is already over

	first := e.expire()
	assert.Equal(t, 1, first["past_due"])
	sub := e.sub(orgID)
	assert.Equal(t, billing.StatusPastDue, sub.Status)
	assert.True(t, epoch.Add(day+3*day).Equal(*sub.GraceUntil), "the grace period is anchored to the period end, so it is long over")
	assert.Equal(t, "free", e.effectivePlan(orgID), "access ended on time: past_due is checked against the clock, not against the last job run")

	second := e.expire()
	assert.Equal(t, 1, second["expired"])
	assert.Equal(t, billing.StatusExpired, e.sub(orgID).Status)
}

func TestExpire_FreeSubscriptionsNeverTransition(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.clock.Advance(50 * 365 * day)

	assert.Equal(t, scheduler.Counts{}, e.expire())
	assert.Equal(t, scheduler.Counts{}, e.applyScheduled())
	assert.Equal(t, scheduler.Counts{}, e.renewals())
	sub := e.sub(orgID)
	assert.Equal(t, billing.StatusActive, sub.Status)
	assert.Equal(t, "free", sub.Plan)
}

func TestExpire_APeriodEndWithAPaidRenewalOrACancellationBelongsToApplyScheduled(t *testing.T) {
	e := newPayEnv(t)
	renewing, canceling := e.newFreeOrg(), e.newFreeOrg()
	e.activePro(renewing, 2*day)
	e.activePro(canceling, 2*day)
	e.renewals()
	require.NoError(t, e.deliver(paidEvent("evt-1", e.renewalInvoices(renewing)[0].Number, 99000)))
	_, err := e.admin.Exec(context.Background(), "UPDATE subscriptions SET cancel_at_period_end = true WHERE organization_id = $1", canceling)
	require.NoError(t, err)
	e.clock.Set(epoch.Add(2*day + time.Minute))

	counts := e.expire()

	assert.Equal(t, scheduler.Counts{"skipped": 2}, counts)
	assert.Equal(t, billing.StatusActive, e.sub(renewing).Status)
	assert.Equal(t, billing.StatusActive, e.sub(canceling).Status)
	assert.Empty(t, e.emails("subscription_past_due"))
}

// --- overdue invoices ---

func TestExpire_InvoicesNobodyCanPayAnyMoreExpire(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activePro(orgID, 10*day)
	upgrade := e.buy(orgID, "business") // checkout invoice due in 7 days
	e.insertRenewal(orgID, "INV-2026-000050", "pro", epoch.Add(10*day))

	e.clock.Set(epoch.Add(7*day - time.Second))
	assert.Equal(t, scheduler.Counts{}, e.expire())
	assert.Equal(t, "open", e.invoiceByNumber(upgrade.Number).Status)

	e.clock.Set(epoch.Add(7 * day))
	counts := e.expire()
	assert.Equal(t, scheduler.Counts{"invoices_expired": 1}, counts)
	assert.Equal(t, "expired", e.invoiceByNumber(upgrade.Number).Status, "the unpaid upgrade, at its due date")
	assert.Equal(t, "open", e.invoiceByNumber("INV-2026-000050").Status, "the renewal is still payable: its period has not even ended")
	assert.Equal(t, billing.StatusActive, e.sub(orgID).Status, "an unpaid upgrade does not touch the subscription")

	e.clock.Set(epoch.Add(13*day - time.Second))
	e.expire()
	assert.Equal(t, "open", e.invoiceByNumber("INV-2026-000050").Status, "a renewal lives through the grace period after its due date")
	e.clock.Set(epoch.Add(13 * day))
	e.expire()
	assert.Equal(t, "expired", e.invoiceByNumber("INV-2026-000050").Status)
}

func TestExpire_PaidInvoicesAreNeverTouched(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	inv := e.buy(orgID, "pro")
	require.NoError(t, e.deliver(paidEvent("evt-1", inv.Number, 99000)))
	e.clock.Advance(100 * day)

	e.expire()

	assert.Equal(t, "paid", e.invoiceByNumber(inv.Number).Status)
}

// --- concurrency ---

func TestExpire_TwoWorkersAtTheSameTimeDoEachTransitionOnce(t *testing.T) {
	e := newPayEnv(t)
	end := epoch.Add(day)
	var past, trials []uuid.UUID
	for range 3 {
		orgID := e.newFreeOrg()
		e.activePro(orgID, day)
		past = append(past, orgID)
		trial := e.newFreeOrg()
		e.setSub(trial, subSpec{Plan: "pro", Status: billing.StatusTrialing, TrialEndsAt: &end, PeriodStart: epoch.AddDate(0, 0, -13), PeriodEnd: &end})
		trials = append(trials, trial)
	}
	e.clock.Set(epoch.Add(2 * day)) // both ended yesterday; the grace period of the paid ones has two days left
	const workers = 5
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = e.svc.ExpireSubscriptions(context.Background())
		}()
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	for _, orgID := range past {
		assert.Equal(t, billing.StatusPastDue, e.sub(orgID).Status)
	}
	for _, orgID := range trials {
		assert.Equal(t, billing.StatusExpired, e.sub(orgID).Status)
	}
	assert.Len(t, e.emails("subscription_past_due"), 3, "one warning each, however many workers ran")
	assert.Len(t, e.emails("subscription_expired"), 3)
}

// --- apply_scheduled ---

func TestApplyScheduled_APaidNextPeriodIsRolledIntoAtThePeriodEnd(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activePro(orgID, 2*day)
	periodEnd := epoch.Add(2 * day)
	e.renewals()
	renewal := e.renewalInvoices(orgID)[0]
	require.NoError(t, e.deliver(paidEvent("evt-1", renewal.Number, 99000)))
	before := e.sub(orgID)

	e.clock.Set(periodEnd.Add(-time.Second))
	assert.Equal(t, scheduler.Counts{}, e.applyScheduled())
	assert.Equal(t, before, e.sub(orgID), "paid ahead, but the period is still the old one")

	e.clock.Set(periodEnd)
	counts := e.applyScheduled()

	assert.Equal(t, scheduler.Counts{"renewed": 1}, counts)
	sub := e.sub(orgID)
	assert.Equal(t, billing.StatusActive, sub.Status)
	assert.True(t, periodEnd.Equal(sub.CurrentPeriodStart))
	assert.True(t, renewal.PeriodEnd.Equal(*sub.CurrentPeriodEnd))

	assert.Equal(t, scheduler.Counts{}, e.applyScheduled(), "the new period is not over: nothing is due again")
	assert.Equal(t, scheduler.Counts{}, e.expire())
	assert.Equal(t, "pro", e.effectivePlan(orgID))
}

func TestApplyScheduled_ABookedDowngradeTakesEffectThroughItsRenewal(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	end := epoch.Add(2 * day)
	e.setSub(orgID, subSpec{Plan: "business", Status: billing.StatusActive, PeriodStart: end.AddDate(0, -1, 0), PeriodEnd: &end})
	res, err := e.svc.ChangePlan(ownerCtx(orgID), "pro")
	require.NoError(t, err)
	require.True(t, res.Scheduled)
	e.renewals()
	renewal := e.renewalInvoices(orgID)[0]
	require.Equal(t, "pro", renewal.Plan, "billed at the plan booked")
	require.NoError(t, e.deliver(paidEvent("evt-1", renewal.Number, 99000)))
	assert.Equal(t, "business", e.sub(orgID).Plan, "until the period ends the bigger plan stays")

	e.clock.Set(end)
	e.applyScheduled()

	sub := e.sub(orgID)
	assert.Equal(t, "pro", sub.Plan)
	assert.Nil(t, sub.PendingPlan, "the booking is consumed")
	assert.Equal(t, billing.StatusActive, sub.Status)
	assert.Equal(t, "pro", e.effectivePlan(orgID))
}

func TestApplyScheduled_ACancellationEndsTheSubscriptionAtThePeriodEnd(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activePro(orgID, 2*day)
	periodEnd := epoch.Add(2 * day)
	e.renewals()
	require.NoError(t, e.svc.CancelSubscription(ownerCtx(orgID)))
	assert.Equal(t, "void", e.renewalInvoices(orgID)[0].Status)

	e.clock.Set(periodEnd.Add(-time.Second))
	assert.Equal(t, scheduler.Counts{}, e.applyScheduled())
	assert.Equal(t, "pro", e.effectivePlan(orgID), "access until the very end")

	e.clock.Set(periodEnd)
	counts := e.applyScheduled()

	assert.Equal(t, scheduler.Counts{"canceled": 1}, counts)
	assert.Equal(t, billing.StatusCanceled, e.sub(orgID).Status)
	assert.Equal(t, "free", e.effectivePlan(orgID))
	assert.Equal(t, scheduler.Counts{}, e.applyScheduled(), "once")
	assert.Equal(t, scheduler.Counts{}, e.expire())
	assert.Empty(t, e.emails("subscription_past_due"), "a canceled subscription is not chased for money")
}

func TestApplyScheduled_ACancelingSubscriptionWhoseNextPeriodWasPaidStillEnds(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activePro(orgID, 2*day)
	e.renewals()
	require.NoError(t, e.deliver(paidEvent("evt-1", e.renewalInvoices(orgID)[0].Number, 99000)))
	require.NoError(t, e.svc.CancelSubscription(ownerCtx(orgID)))
	e.clock.Set(epoch.Add(2 * day))

	assert.Equal(t, scheduler.Counts{"canceled": 1}, e.applyScheduled(), "the owner asked to stop; there are no refunds in Tier 1")
}

func TestApplyScheduled_TwoWorkersAtTheSameTimeApplyEachOnce(t *testing.T) {
	e := newPayEnv(t)
	var renewing, canceling []uuid.UUID
	for range 3 {
		a, b := e.newFreeOrg(), e.newFreeOrg()
		e.activePro(a, 2*day)
		e.activePro(b, 2*day)
		renewing, canceling = append(renewing, a), append(canceling, b)
	}
	e.renewals()
	for i, orgID := range renewing {
		require.NoError(t, e.deliver(paidEvent("evt-"+orgID.String(), e.renewalInvoices(orgID)[0].Number, 99000)), i)
	}
	for _, orgID := range canceling {
		require.NoError(t, e.svc.CancelSubscription(ownerCtx(orgID)))
	}
	e.clock.Set(epoch.Add(2 * day))
	const workers = 5
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = e.svc.ApplyScheduled(context.Background())
		}()
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	for _, orgID := range renewing {
		assert.Equal(t, billing.StatusActive, e.sub(orgID).Status)
		assert.True(t, epoch.Add(2*day).Equal(e.sub(orgID).CurrentPeriodStart))
	}
	for _, orgID := range canceling {
		assert.Equal(t, billing.StatusCanceled, e.sub(orgID).Status)
	}
	assert.Equal(t, scheduler.Counts{}, e.applyScheduled())
}

func TestRegisterJobs_RegistersExpireAndApplyScheduled(t *testing.T) {
	e := newPayEnv(t)
	sched := newTestScheduler(t, e)
	require.NoError(t, e.svc.RegisterJobs(sched))

	for _, name := range []string{billing.JobExpire, billing.JobApplyScheduled} {
		res, err := sched.RunOnce(context.Background(), name)
		require.NoError(t, err, name)
		assert.True(t, res.Ran, name)
	}
}
