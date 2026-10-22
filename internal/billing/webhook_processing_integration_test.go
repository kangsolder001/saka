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
	"github.com/kangsolder001/saka/internal/billing/payment"
)

// buy checks the organization out on a plan and returns the invoice.
func (e *payEnv) buy(orgID uuid.UUID, plan string) billing.Invoice {
	e.t.Helper()
	res, err := e.svc.Checkout(ownerCtx(orgID), plan)
	require.NoError(e.t, err)
	return res.Invoice
}

// insertRenewal puts an open renewal invoice for the period after periodEnd on
// the organization's subscription.
func (e *payEnv) insertRenewal(orgID uuid.UUID, number, plan string, periodEnd time.Time) {
	e.t.Helper()
	_, err := e.admin.Exec(context.Background(), `
		INSERT INTO invoices (organization_id, subscription_id, number, kind, plan_id, amount, currency, status, period_start, period_end, due_at, provider)
		SELECT $1, s.id, $2, 'renewal', p.id, p.price_amount, p.currency, 'open', $4::timestamptz, $4::timestamptz + interval '1 month', $4::timestamptz, 'fake'
		FROM plans p, subscriptions s WHERE p.code = $3 AND s.organization_id = $1`, orgID, number, plan, periodEnd)
	require.NoError(e.t, err)
}

func requireProcessedClean(t *testing.T, ev eventRow) {
	t.Helper()
	assert.NotNil(t, ev.ProcessedAt, "processed")
	assert.Nil(t, ev.Error, "and nothing for a person to look at")
}

func requireFlagged(t *testing.T, ev eventRow, contains string) {
	t.Helper()
	assert.NotNil(t, ev.ProcessedAt, "processed: the gateway must not retry it")
	require.NotNil(t, ev.Error, "flagged for a person")
	assert.Contains(t, *ev.Error, contains)
}

func TestWebhook_ThePaymentOfATrialInvoiceActivatesThePlanFromThePaymentTime(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	inv := e.buy(orgID, "pro")
	require.Equal(t, billing.StatusTrialing, e.sub(orgID).Status)
	e.clock.Advance(5 * day)

	require.NoError(t, e.deliver(paidEvent("evt-1", inv.Number, 99000)))

	sub := e.sub(orgID)
	assert.Equal(t, billing.StatusActive, sub.Status)
	assert.Equal(t, "pro", sub.Plan)
	assert.True(t, e.clock.Now().Equal(sub.CurrentPeriodStart), "paying early forfeits the rest of the trial: the period starts now")
	assert.True(t, e.clock.Now().AddDate(0, 1, 0).Equal(*sub.CurrentPeriodEnd))
	assert.NotNil(t, sub.TrialEndsAt, "the trial stays marked as used")

	paid := e.invoiceByNumber(inv.Number)
	assert.Equal(t, "paid", paid.Status)
	require.NotNil(t, paid.PaidAt)
	assert.True(t, e.clock.Now().Equal(*paid.PaidAt))
	assert.True(t, e.clock.Now().Equal(paid.PeriodStart), "the invoice records the period actually paid for")
	assert.True(t, e.clock.Now().AddDate(0, 1, 0).Equal(paid.PeriodEnd))
	require.NotNil(t, paid.ProviderRef)
	assert.Equal(t, "txn-evt-1", *paid.ProviderRef, "the gateway's own id replaces the order reference")

	requireProcessedClean(t, e.events()[0])
	emails := e.emails("payment_succeeded")
	require.Len(t, emails, 1)
	assert.Equal(t, e.ownerEmail(orgID), emails[0].To)
	assert.Equal(t, inv.Number, emails[0].Data["invoice_number"])
	assert.Equal(t, billing.FormatDate(e.clock.Now().AddDate(0, 1, 0)), emails[0].Data["period_end"])
}

func TestWebhook_APaidUpgradeMovesToTheNewPlanAndVoidsWhatElseWasWaiting(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activePro(orgID, 10*day)
	e.insertRenewal(orgID, "INV-2026-009999", "pro", epoch.Add(10*day))
	inv := e.buy(orgID, "business")

	require.NoError(t, e.deliver(paidEvent("evt-1", inv.Number, 299000)))

	sub := e.sub(orgID)
	assert.Equal(t, "business", sub.Plan)
	assert.Equal(t, billing.StatusActive, sub.Status)
	assert.True(t, epoch.Equal(sub.CurrentPeriodStart), "the period restarts now; nothing is credited (spec §8.4)")
	assert.Equal(t, "void", e.invoiceByNumber("INV-2026-009999").Status, "the renewal for the old plan and period is moot")
}

func TestWebhook_AnExpiredOrganizationComesBackWhenItsCheckoutIsPaid(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	end := epoch.AddDate(0, 0, -10)
	e.setSub(orgID, subSpec{Plan: "pro", Status: billing.StatusExpired, TrialEndsAt: at(epoch.AddDate(0, -2, 0)), PeriodStart: end.AddDate(0, -1, 0), PeriodEnd: &end})
	inv := e.buy(orgID, "pro")

	require.NoError(t, e.deliver(paidEvent("evt-1", inv.Number, 99000)))

	sub := e.sub(orgID)
	assert.Equal(t, billing.StatusActive, sub.Status)
	assert.True(t, epoch.Equal(sub.CurrentPeriodStart))
	assert.Nil(t, sub.GraceUntil)
}

func TestWebhook_ARenewalPaidWhilePastDueContinuesFromTheOldPeriodEnd(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	periodEnd := epoch.Add(-day) // ended yesterday, unpaid
	graceUntil := periodEnd.Add(3 * day)
	e.setSub(orgID, subSpec{Plan: "pro", Status: billing.StatusPastDue, PeriodStart: periodEnd.AddDate(0, -1, 0), PeriodEnd: &periodEnd, GraceUntil: &graceUntil})
	e.insertRenewal(orgID, "INV-2026-000050", "pro", periodEnd)

	require.NoError(t, e.deliver(paidEvent("evt-1", "INV-2026-000050", 99000)))

	sub := e.sub(orgID)
	assert.Equal(t, billing.StatusActive, sub.Status)
	assert.Nil(t, sub.GraceUntil)
	assert.True(t, periodEnd.Equal(sub.CurrentPeriodStart), "no gap and no free days")
	wantEnd, err := billing.AddInterval(periodEnd, "month")
	require.NoError(t, err)
	assert.True(t, wantEnd.Equal(*sub.CurrentPeriodEnd))
	assert.Equal(t, "paid", e.invoiceByNumber("INV-2026-000050").Status)
}

func TestWebhook_ARenewalPaidBeforeThePeriodEndsIsRecordedAndTheSubscriptionWaits(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activePro(orgID, 2*day)
	before := e.sub(orgID)
	e.insertRenewal(orgID, "INV-2026-000050", "pro", epoch.Add(2*day))

	require.NoError(t, e.deliver(paidEvent("evt-1", "INV-2026-000050", 99000)))

	assert.Equal(t, before, e.sub(orgID), "the next period begins when this one ends")
	assert.Equal(t, "paid", e.invoiceByNumber("INV-2026-000050").Status)
	assert.Len(t, e.emails("payment_succeeded"), 1)
	requireProcessedClean(t, e.events()[0])
}

func TestWebhook_TheSamePaymentDeliveredTwiceIsAppliedOnce(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	inv := e.buy(orgID, "pro")
	ev := paidEvent("evt-1", inv.Number, 99000)

	require.NoError(t, e.deliver(ev))
	afterFirst := e.sub(orgID)
	e.clock.Advance(time.Hour)
	require.NoError(t, e.deliver(ev))

	assert.Equal(t, afterFirst, e.sub(orgID), "the second delivery changed nothing, not even the period start")
	assert.Len(t, e.emails("payment_succeeded"), 1, "one confirmation email")
	assert.Len(t, e.events(), 1)
}

func TestWebhook_DuplicateDeliveriesAtTheSameTimeAreAppliedOnce(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	inv := e.buy(orgID, "pro")
	ev := paidEvent("evt-1", inv.Number, 99000)
	const deliveries = 6
	errs := make([]error, deliveries)
	var wg sync.WaitGroup
	for i := range deliveries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = e.deliver(ev)
		}()
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, billing.StatusActive, e.sub(orgID).Status)
	assert.Len(t, e.emails("payment_succeeded"), 1)
	assert.Equal(t, 1, e.countRows("SELECT count(*) FROM invoices WHERE status = 'paid'"))
	requireProcessedClean(t, e.events()[0])
}

func TestWebhook_TheSecondNotificationOfOnePaymentUnderAnotherEventIDChangesNothing(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	inv := e.buy(orgID, "pro")
	require.NoError(t, e.deliver(paidEvent("capture-1", inv.Number, 99000)))
	afterFirst := e.sub(orgID)
	e.clock.Advance(time.Hour)

	require.NoError(t, e.deliver(paidEvent("settlement-1", inv.Number, 99000)))

	assert.Equal(t, afterFirst, e.sub(orgID))
	assert.Len(t, e.emails("payment_succeeded"), 1)
	events := e.events()
	require.Len(t, events, 2)
	requireProcessedClean(t, events[1])
}

func TestWebhook_APaymentForTheWrongAmountOrCurrencyActivatesNothing(t *testing.T) {
	for name, ev := range map[string]payment.WebhookEvent{
		"less":     {EventID: "evt-1", State: payment.StatePaid, Amount: 1000, Currency: "IDR"},
		"more":     {EventID: "evt-1", State: payment.StatePaid, Amount: 990000, Currency: "IDR"},
		"currency": {EventID: "evt-1", State: payment.StatePaid, Amount: 99000, Currency: "USD"},
		"nothing":  {EventID: "evt-1", State: payment.StatePaid},
	} {
		t.Run(name, func(t *testing.T) {
			e := newPayEnv(t)
			orgID := e.newFreeOrg()
			inv := e.buy(orgID, "pro")
			ev.InvoiceNumber = inv.Number

			require.NoError(t, e.deliver(ev), "answered with success: retrying cannot make the amount right")

			assert.Equal(t, "open", e.invoiceByNumber(inv.Number).Status)
			assert.Equal(t, billing.StatusTrialing, e.sub(orgID).Status, "still only the trial")
			assert.Empty(t, e.emails("payment_succeeded"))
			requireFlagged(t, e.events()[0], "amount mismatch")
		})
	}
}

func TestWebhook_ApaymentForAnInvoiceThatIsAlreadyClosedIsFlaggedNotActivated(t *testing.T) {
	for _, status := range []string{"void", "expired"} {
		t.Run(status, func(t *testing.T) {
			e := newPayEnv(t)
			orgID := e.newFreeOrg()
			inv := e.buy(orgID, "pro")
			_, err := e.admin.Exec(context.Background(), "UPDATE invoices SET status = $2 WHERE number = $1", inv.Number, status)
			require.NoError(t, err)

			require.NoError(t, e.deliver(paidEvent("evt-1", inv.Number, 99000)))

			assert.Equal(t, status, e.invoiceByNumber(inv.Number).Status, "the invoice does not reopen")
			assert.Equal(t, billing.StatusTrialing, e.sub(orgID).Status, "the subscription was left as it was")
			assert.Empty(t, e.emails("payment_succeeded"))
			requireFlagged(t, e.events()[0], status)
		})
	}
}

func TestWebhook_APaymentForAnUnknownInvoiceIsFlagged(t *testing.T) {
	e := newPayEnv(t)

	require.NoError(t, e.deliver(paidEvent("evt-1", "INV-2026-424242", 99000)))

	requireFlagged(t, e.events()[0], "INV-2026-424242")
}

func TestWebhook_ARenewalPaidForASubscriptionThatEndedKeepsTheMoneyAndFlagsIt(t *testing.T) {
	for _, status := range []billing.Status{billing.StatusExpired, billing.StatusCanceled} {
		t.Run(string(status), func(t *testing.T) {
			e := newPayEnv(t)
			orgID := e.newFreeOrg()
			end := epoch.AddDate(0, 0, -5)
			e.setSub(orgID, subSpec{Plan: "pro", Status: status, TrialEndsAt: at(epoch.AddDate(0, -2, 0)), PeriodStart: end.AddDate(0, -1, 0), PeriodEnd: &end})
			e.insertRenewal(orgID, "INV-2026-000050", "pro", end)

			require.NoError(t, e.deliver(paidEvent("evt-1", "INV-2026-000050", 99000)))

			assert.Equal(t, "paid", e.invoiceByNumber("INV-2026-000050").Status, "the payment is on record")
			assert.Equal(t, status, e.sub(orgID).Status, "but it does not bring the subscription back by itself")
			requireFlagged(t, e.events()[0], "settle it by hand")
			assert.Empty(t, e.emails("payment_succeeded"))
		})
	}
}

func TestWebhook_LateOrRepeatedStatesNeverMoveAnInvoiceBackwards(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	inv := e.buy(orgID, "pro")
	require.NoError(t, e.deliver(paidEvent("evt-paid", inv.Number, 99000)))

	for i, state := range []payment.State{payment.StateExpired, payment.StateCanceled, payment.StatePending, payment.StateFailed} {
		ev := payment.WebhookEvent{EventID: "late-" + string(state), InvoiceNumber: inv.Number, State: state, Amount: 99000, Currency: "IDR"}
		require.NoError(t, e.deliver(ev), "event %d", i)
	}

	assert.Equal(t, "paid", e.invoiceByNumber(inv.Number).Status)
	assert.Equal(t, billing.StatusActive, e.sub(orgID).Status)
}

func TestWebhook_ExpiredAndCanceledNotificationsCloseAnOpenInvoice(t *testing.T) {
	for state, want := range map[payment.State]string{payment.StateExpired: "expired", payment.StateCanceled: "void"} {
		t.Run(string(state), func(t *testing.T) {
			e := newPayEnv(t)
			orgID := e.newFreeOrg()
			inv := e.buy(orgID, "pro")

			require.NoError(t, e.deliver(payment.WebhookEvent{EventID: "evt-1", InvoiceNumber: inv.Number, State: state}))

			assert.Equal(t, want, e.invoiceByNumber(inv.Number).Status)
			assert.Equal(t, billing.StatusTrialing, e.sub(orgID).Status, "the trial runs on; only billing.expire ends it")
			requireProcessedClean(t, e.events()[0])
		})
	}
}

func TestWebhook_PendingAndFailedAttemptsLeaveTheInvoiceOpen(t *testing.T) {
	for _, state := range []payment.State{payment.StatePending, payment.StateFailed} {
		t.Run(string(state), func(t *testing.T) {
			e := newPayEnv(t)
			orgID := e.newFreeOrg()
			inv := e.buy(orgID, "pro")

			require.NoError(t, e.deliver(payment.WebhookEvent{EventID: "evt-1", InvoiceNumber: inv.Number, State: state}))

			assert.Equal(t, "open", e.invoiceByNumber(inv.Number).Status)
			requireProcessedClean(t, e.events()[0])
		})
	}
}

func TestWebhook_ARefundIsFlaggedAndChangesNothing(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	inv := e.buy(orgID, "pro")
	require.NoError(t, e.deliver(paidEvent("evt-paid", inv.Number, 99000)))

	require.NoError(t, e.deliver(payment.WebhookEvent{EventID: "evt-refund", InvoiceNumber: inv.Number, State: payment.StateRefunded}))

	assert.Equal(t, "paid", e.invoiceByNumber(inv.Number).Status)
	assert.Equal(t, billing.StatusActive, e.sub(orgID).Status)
	requireFlagged(t, e.events()[1], "refund")
}

func TestWebhook_AnEventForAnotherProvidersInvoiceIsFlagged(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	inv := e.buy(orgID, "pro")
	_, err := e.admin.Exec(context.Background(), "UPDATE invoices SET provider = 'midtrans' WHERE number = $1", inv.Number)
	require.NoError(t, err)

	require.NoError(t, e.deliver(paidEvent("evt-1", inv.Number, 99000)))

	assert.Equal(t, "open", e.invoiceByNumber(inv.Number).Status)
	requireFlagged(t, e.events()[0], "provider")
}

// A failure while processing rolls everything back, records why, answers with
// an error (so the gateway retries) and leaves the event reprocessable.
func TestWebhook_AFailureRollsBackRecordsTheErrorAndTheRetryWorks(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	inv := e.buy(orgID, "pro")
	// The owner's address cannot be queued, so the confirmation email fails after
	// the invoice and the subscription were updated.
	_, err := e.admin.Exec(context.Background(), "UPDATE users SET email = 'not-an-address' WHERE id = (SELECT owner_id FROM organizations WHERE id = $1)", orgID)
	require.NoError(t, err)
	ev := paidEvent("evt-1", inv.Number, 99000)

	err = e.deliver(ev)

	require.Error(t, err)
	assert.Equal(t, "open", e.invoiceByNumber(inv.Number).Status, "the invoice update was rolled back with the rest")
	assert.Equal(t, billing.StatusTrialing, e.sub(orgID).Status)
	events := e.events()
	require.Len(t, events, 1, "the event itself stays on record")
	assert.Nil(t, events[0].ProcessedAt, "unprocessed, so the retry processes it")
	require.NotNil(t, events[0].Error)
	assert.Contains(t, *events[0].Error, "email")

	// The cause is fixed; the gateway retries the same notification.
	_, err = e.admin.Exec(context.Background(), "UPDATE users SET email = 'owner@example.com' WHERE id = (SELECT owner_id FROM organizations WHERE id = $1)", orgID)
	require.NoError(t, err)
	require.NoError(t, e.deliver(ev))

	assert.Equal(t, "paid", e.invoiceByNumber(inv.Number).Status)
	assert.Equal(t, billing.StatusActive, e.sub(orgID).Status)
	events = e.events()
	require.Len(t, events, 1, "the retry reuses the stored event")
	assert.NotNil(t, events[0].ProcessedAt)
	assert.Nil(t, events[0].Error, "a successful retry clears the failure")
	assert.Len(t, e.emails("payment_succeeded"), 1)
}

func TestWebhook_TheOrganizationsOwnerStillGetsTheReceiptWhenTheOrganizationWasDeleted(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	inv := e.buy(orgID, "pro")
	_, err := e.admin.Exec(context.Background(), "UPDATE organizations SET deleted_at = now() WHERE id = $1", orgID)
	require.NoError(t, err)

	require.NoError(t, e.deliver(paidEvent("evt-1", inv.Number, 99000)))

	assert.Equal(t, "paid", e.invoiceByNumber(inv.Number).Status)
	assert.Len(t, e.emails("payment_succeeded"), 1)
}
