//go:build integration

package billing_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/billing"
	"github.com/kangsolder001/saka/internal/billing/payment/fake"
	"github.com/kangsolder001/saka/internal/platform/scheduler"
)

func (e *payEnv) reminders() scheduler.Counts {
	e.t.Helper()
	counts, err := e.svc.SendReminders(context.Background())
	require.NoError(e.t, err)
	return counts
}

func TestReminders_TheTrialsFirstInvoiceGetsTrialEndingThreeDaysAndOneDayBeforeTheEnd(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	inv := e.buy(orgID, "pro")
	trialEnd := epoch.AddDate(0, 0, 14)

	e.clock.Set(trialEnd.Add(-3*day - time.Second))
	assert.Equal(t, scheduler.Counts{}, e.reminders(), "just outside the window")

	e.clock.Set(trialEnd.Add(-3 * day))
	assert.Equal(t, scheduler.Counts{"sent": 1}, e.reminders())
	emails := e.emails("trial_ending")
	require.Len(t, emails, 1)
	assert.Equal(t, e.ownerEmail(orgID), emails[0].To)
	assert.EqualValues(t, 3, emails[0].Data["days_left"])
	assert.Equal(t, billing.FormatDate(trialEnd), emails[0].Data["trial_end_date"])
	assert.Equal(t, "Rp99.000", emails[0].Data["amount"])
	assert.Equal(t, fake.PaymentURL(inv.Number), emails[0].Data["payment_url"], "the way to keep the plan")
	assert.Empty(t, e.emails("payment_reminder"))

	assert.Equal(t, scheduler.Counts{}, e.reminders(), "the same reminder is not sent again")
	e.clock.Set(trialEnd.Add(-2 * day))
	assert.Equal(t, scheduler.Counts{}, e.reminders(), "between the windows there is nothing to say")

	e.clock.Set(trialEnd.Add(-day))
	assert.Equal(t, scheduler.Counts{"sent": 1}, e.reminders())
	assert.Len(t, e.emails("trial_ending"), 2)
	assert.EqualValues(t, 1, e.emails("trial_ending")[1].Data["days_left"])

	e.clock.Set(trialEnd.Add(-time.Hour))
	assert.Equal(t, scheduler.Counts{}, e.reminders(), "both reminders were sent")
	assert.Len(t, e.emails("trial_ending"), 2)
}

func TestReminders_AnOrdinaryInvoiceGetsPaymentReminders(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	past := epoch.AddDate(0, -3, 0)
	e.setSub(orgID, subSpec{Plan: "free", Status: billing.StatusActive, TrialEndsAt: &past, PeriodStart: past})
	inv := e.buy(orgID, "pro") // due in 7 days

	e.clock.Advance(4 * day) // 3 days left
	assert.Equal(t, scheduler.Counts{"sent": 1}, e.reminders())
	e.clock.Advance(2 * day) // 1 day left
	assert.Equal(t, scheduler.Counts{"sent": 1}, e.reminders())

	emails := e.emails("payment_reminder")
	require.Len(t, emails, 2)
	assert.Equal(t, inv.Number, emails[0].Data["invoice_number"])
	assert.EqualValues(t, 3, emails[0].Data["days_left"])
	assert.EqualValues(t, 1, emails[1].Data["days_left"])
	assert.Equal(t, billing.FormatDate(epoch.Add(7*day)), emails[1].Data["due_date"])
	assert.Empty(t, e.emails("trial_ending"))
}

func TestReminders_ARenewalCreatedInsideTheWindowIsNotRemindedOfTheSameThing(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activePro(orgID, 2*day)
	e.renewals() // issued 2 days before it falls due: that email is the notice for the 3 day window

	assert.Equal(t, scheduler.Counts{}, e.reminders(), "no reminder straight after the invoice email")

	e.clock.Set(epoch.Add(day))
	assert.Equal(t, scheduler.Counts{"sent": 1}, e.reminders(), "the last day still gets its reminder")
	assert.Len(t, e.emails("payment_reminder"), 1)
	assert.Len(t, e.emails("invoice_created"), 1)
}

func TestReminders_AWorkerThatWasDownSendsOneEmailForBothWindows(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.buy(orgID, "pro")

	e.clock.Set(epoch.AddDate(0, 0, 14).Add(-12 * time.Hour)) // inside both windows at once
	assert.Equal(t, scheduler.Counts{"sent": 1}, e.reminders())
	assert.Equal(t, scheduler.Counts{}, e.reminders())

	assert.Len(t, e.emails("trial_ending"), 1)
	row := e.invoices(orgID)[0]
	require.NotNil(t, row.Reminded3d)
	require.NotNil(t, row.Reminded1d)
}

func TestReminders_NothingForInvoicesThatAreNotWaitingForPaymentOrAreLate(t *testing.T) {
	e := newPayEnv(t)
	paid, voided, past := e.newFreeOrg(), e.newFreeOrg(), e.newFreeOrg()
	paidInv, voidInv, pastInv := e.buy(paid, "pro"), e.buy(voided, "pro"), e.buy(past, "pro")
	require.NoError(t, e.deliver(paidEvent("evt-1", paidInv.Number, 99000)))
	_, err := e.admin.Exec(context.Background(), "UPDATE invoices SET status = 'void' WHERE number = $1", voidInv.Number)
	require.NoError(t, err)

	e.clock.Set(epoch.AddDate(0, 0, 14).Add(-day)) // inside the windows
	e.reminders()
	assert.Len(t, e.emails("trial_ending"), 1, "only the invoice still open")
	assert.Equal(t, e.ownerEmail(past), e.emails("trial_ending")[0].To)

	e.clock.Set(epoch.AddDate(0, 0, 15))
	_, err = e.admin.Exec(context.Background(), "UPDATE invoices SET reminder_1d_sent_at = NULL, reminder_3d_sent_at = NULL WHERE number = $1", pastInv.Number)
	require.NoError(t, err)
	assert.Equal(t, scheduler.Counts{}, e.reminders(), "no reminder once it is overdue; that is the past due email's job")
}

func TestReminders_NotForASubscriptionThatNoLongerRuns(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.buy(orgID, "pro")
	require.NoError(t, e.svc.CancelSubscription(ownerCtx(orgID))) // a trial may be canceled too
	_, err := e.admin.Exec(context.Background(), "UPDATE subscriptions SET status = 'canceled' WHERE organization_id = $1", orgID)
	require.NoError(t, err)
	e.clock.Set(epoch.AddDate(0, 0, 14).Add(-day))

	assert.Equal(t, scheduler.Counts{"skipped": 1}, e.reminders())
	assert.Empty(t, e.emails("trial_ending"))
	assert.Empty(t, e.emails("payment_reminder"))
}

func TestReminders_TwoWorkersAtTheSameTimeSendEachOnce(t *testing.T) {
	e := newPayEnv(t)
	for range 4 {
		e.buy(e.newFreeOrg(), "pro")
	}
	e.clock.Set(epoch.AddDate(0, 0, 14).Add(-3 * day))
	const workers = 5
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = e.svc.SendReminders(context.Background())
		}()
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	assert.Len(t, e.emails("trial_ending"), 4, "one per invoice however many workers ran")
}

func TestReminders_ARemindedInvoiceThatFailsToQueueItsEmailIsNotMarkedSent(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.buy(orgID, "pro")
	_, err := e.admin.Exec(context.Background(), "UPDATE users SET email = 'not-an-address' WHERE id = (SELECT owner_id FROM organizations WHERE id = $1)", orgID)
	require.NoError(t, err)
	e.clock.Set(epoch.AddDate(0, 0, 14).Add(-3 * day))

	counts, err := e.svc.SendReminders(context.Background())

	require.Error(t, err)
	assert.Equal(t, scheduler.Counts{"failed": 1}, counts)
	assert.Nil(t, e.invoices(orgID)[0].Reminded3d, "the marker is written with the email or not at all")

	_, err = e.admin.Exec(context.Background(), "UPDATE users SET email = 'owner@example.com' WHERE id = (SELECT owner_id FROM organizations WHERE id = $1)", orgID)
	require.NoError(t, err)
	assert.Equal(t, scheduler.Counts{"sent": 1}, e.reminders(), "and it is sent at the next run")
}
