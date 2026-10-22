//go:build integration

package billing_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/billing"
	"github.com/kangsolder001/saka/internal/billing/payment/fake"
	"github.com/kangsolder001/saka/internal/org"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/pagination"
)

func TestCheckout_AFreeOrganizationStartsTheTrialAndGetsTheFirstInvoice(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()

	res, err := e.svc.Checkout(ownerCtx(orgID), "pro")

	require.NoError(t, err)
	assert.True(t, res.TrialStarted)
	assert.False(t, res.Reused)

	trialEnd := epoch.AddDate(0, 0, 14)
	sub := e.sub(orgID)
	assert.Equal(t, billing.StatusTrialing, sub.Status, "the trial starts at once: no payment is needed for it")
	assert.Equal(t, "pro", sub.Plan)
	assert.True(t, trialEnd.Equal(*sub.TrialEndsAt))
	assert.True(t, trialEnd.Equal(*sub.CurrentPeriodEnd))

	invoices := e.invoices(orgID)
	require.Len(t, invoices, 1)
	inv := invoices[0]
	assert.Equal(t, "INV-2026-000001", inv.Number)
	assert.Equal(t, "checkout", inv.Kind)
	assert.Equal(t, "pro", inv.Plan)
	assert.Equal(t, int64(99000), inv.Amount, "the price of the plan, from the plans table")
	assert.Equal(t, "IDR", inv.Currency)
	assert.Equal(t, "open", inv.Status)
	assert.Equal(t, "fake", inv.Provider)
	assert.True(t, trialEnd.Equal(inv.DueAt), "the first invoice is due when the trial ends")
	require.NotNil(t, inv.PaymentURL)
	assert.Equal(t, fake.PaymentURL("INV-2026-000001"), *inv.PaymentURL)
	assert.Nil(t, inv.PaidAt)
	assert.Nil(t, inv.Reminded3d, "the reminders are still to come")
	assert.Equal(t, res.Invoice.Number, inv.Number)
	assert.Equal(t, inv.PaymentURL, res.Invoice.PaymentURL)
}

func TestCheckout_AsksTheGatewayForThePlansPriceNotAnythingTheClientSent(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	_, err := e.admin.Exec(context.Background(), "UPDATE plans SET price_amount = 125000 WHERE code = 'business'")
	require.NoError(t, err)

	_, err = e.svc.Checkout(ownerCtx(orgID), "business")

	require.NoError(t, err)
	req, err := e.gateway.Last()
	require.NoError(t, err)
	assert.Equal(t, int64(125000), req.Amount)
	assert.Equal(t, "IDR", req.Currency)
	assert.Equal(t, "INV-2026-000001", req.InvoiceNumber)
	assert.Equal(t, e.ownerEmail(orgID), req.Customer.Email)
	assert.Equal(t, "n", req.Customer.Name)
	assert.True(t, epoch.AddDate(0, 0, 14).Equal(req.ExpiresAt), "the link dies when the invoice falls due")
	assert.Contains(t, req.Description, "Business")
}

func TestCheckout_AnIssuedInvoiceKeepsItsPriceWhenThePlanLaterChanges(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	res, err := e.svc.Checkout(ownerCtx(orgID), "pro")
	require.NoError(t, err)

	_, err = e.admin.Exec(context.Background(), "UPDATE plans SET price_amount = 1 WHERE code = 'pro'")
	require.NoError(t, err)

	inv, err := e.svc.GetInvoice(ownerCtx(orgID), res.Invoice.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(99000), inv.Amount)
}

func TestCheckout_QueuesTheInvoiceEmailForTheOwnerInTheSameTransaction(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()

	_, err := e.svc.Checkout(ownerCtx(orgID), "pro")

	require.NoError(t, err)
	emails := e.emails("invoice_created")
	require.Len(t, emails, 1)
	assert.Equal(t, e.ownerEmail(orgID), emails[0].To)
	assert.Equal(t, "INV-2026-000001", emails[0].Data["invoice_number"])
	assert.Equal(t, "Rp99.000", emails[0].Data["amount"])
	assert.Equal(t, "Pro", emails[0].Data["plan_name"])
	assert.Equal(t, fake.PaymentURL("INV-2026-000001"), emails[0].Data["payment_url"])
	assert.Equal(t, "Org", emails[0].Data["organization_name"])
	assert.Equal(t, billing.FormatDate(epoch.AddDate(0, 0, 14)), emails[0].Data["due_date"])
}

func TestCheckout_ATrialIsOnlyGivenOnce(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	// It trialed Pro long ago, never paid, and is back on Free.
	past := epoch.AddDate(0, -3, 0)
	e.setSub(orgID, subSpec{Plan: "free", Status: billing.StatusActive, TrialEndsAt: &past, PeriodStart: past})

	res, err := e.svc.Checkout(ownerCtx(orgID), "pro")

	require.NoError(t, err)
	assert.False(t, res.TrialStarted)
	sub := e.sub(orgID)
	assert.Equal(t, billing.StatusActive, sub.Status, "nothing changes until the invoice is paid")
	assert.Equal(t, "free", sub.Plan)
	inv := e.invoices(orgID)[0]
	assert.True(t, epoch.Add(7*day).Equal(inv.DueAt), "a plain invoice falls due after billing.invoice_due_days")
}

func TestCheckout_APlanWithoutATrialJustGetsAnInvoice(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.setTrialDays("pro", 0)

	res, err := e.svc.Checkout(ownerCtx(orgID), "pro")

	require.NoError(t, err)
	assert.False(t, res.TrialStarted)
	assert.Equal(t, billing.StatusActive, e.sub(orgID).Status)
	assert.Equal(t, "free", e.sub(orgID).Plan)
}

func TestCheckout_CalledAgainReturnsTheInvoiceStillWaitingForPayment(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	first, err := e.svc.Checkout(ownerCtx(orgID), "pro")
	require.NoError(t, err)

	second, err := e.svc.Checkout(ownerCtx(orgID), "pro")

	require.NoError(t, err)
	assert.True(t, second.Reused)
	assert.False(t, second.TrialStarted)
	assert.Equal(t, first.Invoice.ID, second.Invoice.ID)
	assert.Len(t, e.invoices(orgID), 1)
	assert.Len(t, e.gateway.Created(), 1, "the gateway was asked once")
	assert.Len(t, e.emails("invoice_created"), 1, "and the owner was told once")
}

func TestCheckout_ParallelRequestsCreateOneInvoice(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	const callers = 6
	results := make([]billing.CheckoutResult, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = e.svc.Checkout(ownerCtx(orgID), "pro")
		}()
	}
	wg.Wait()

	reused := 0
	for i := range callers {
		require.NoError(t, errs[i])
		assert.Equal(t, results[0].Invoice.ID, results[i].Invoice.ID)
		if results[i].Reused {
			reused++
		}
	}
	assert.Equal(t, callers-1, reused, "exactly one request created it")
	assert.Len(t, e.invoices(orgID), 1)
	assert.Len(t, e.gateway.Created(), 1)
}

func TestCheckout_AnotherPlanReplacesTheUnpaidCheckout(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	first, err := e.svc.Checkout(ownerCtx(orgID), "pro")
	require.NoError(t, err)

	second, err := e.svc.Checkout(ownerCtx(orgID), "business")

	require.NoError(t, err)
	assert.False(t, second.Reused)
	assert.False(t, second.TrialStarted, "the trial was already used by the first checkout")
	assert.Equal(t, billing.StatusTrialing, e.sub(orgID).Status)
	assert.Equal(t, "pro", e.sub(orgID).Plan, "the trial keeps its plan until the new one is paid")
	invoices := e.invoices(orgID)
	require.Len(t, invoices, 2)
	assert.Equal(t, first.Invoice.Number, invoices[0].Number)
	assert.Equal(t, "void", invoices[0].Status, "one live checkout at a time")
	assert.Equal(t, "open", invoices[1].Status)
	assert.Equal(t, "business", invoices[1].Plan)
	assert.Equal(t, int64(299000), invoices[1].Amount)
}

func TestCheckout_APaymentLinkThatHasExpiredIsNotHandedOutAgain(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	past := epoch.AddDate(0, -3, 0)
	e.setSub(orgID, subSpec{Plan: "free", Status: billing.StatusActive, TrialEndsAt: &past, PeriodStart: past})
	first, err := e.svc.Checkout(ownerCtx(orgID), "pro")
	require.NoError(t, err)

	e.clock.Advance(8 * day) // past the 7 days it was payable
	second, err := e.svc.Checkout(ownerCtx(orgID), "pro")

	require.NoError(t, err)
	assert.False(t, second.Reused)
	assert.NotEqual(t, first.Invoice.ID, second.Invoice.ID)
	assert.Equal(t, "void", e.invoiceByNumber(first.Invoice.Number).Status)
}

func TestCheckout_AnOrganizationAlreadyOnThePlanIsToldSo(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activePro(orgID, 20*day)

	_, err := e.svc.Checkout(ownerCtx(orgID), "pro")

	require.Error(t, err)
	assert.True(t, apperr.IsCode(err, apperr.CodeConflict), err.Error())
	assert.Empty(t, e.invoices(orgID))
	assert.Empty(t, e.gateway.Created())
}

func TestCheckout_ReturnsTheRenewalInvoiceOfThePlanThatIsAboutToBeDue(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.activePro(orgID, 2*day)
	subID := e.sub(orgID).ID
	_, err := e.admin.Exec(context.Background(), `
		INSERT INTO invoices (organization_id, subscription_id, number, kind, plan_id, amount, currency, status, period_start, period_end, due_at, provider, payment_url)
		SELECT $1, $2, 'INV-2026-009999', 'renewal', p.id, 99000, 'IDR', 'open', $3, $4, $3, 'fake', 'https://pay.fake.test/INV-2026-009999'
		FROM plans p WHERE p.code = 'pro'`, orgID, subID, epoch.Add(2*day), epoch.Add(2*day).AddDate(0, 1, 0))
	require.NoError(t, err)

	res, err := e.svc.Checkout(ownerCtx(orgID), "pro")

	require.NoError(t, err)
	assert.True(t, res.Reused, "paying the upcoming renewal early is allowed")
	assert.Equal(t, "INV-2026-009999", res.Invoice.Number)
	assert.Empty(t, e.gateway.Created())
}

func TestCheckout_AnExpiredOrCanceledOrganizationCanSubscribeAgain(t *testing.T) {
	for _, status := range []billing.Status{billing.StatusExpired, billing.StatusCanceled} {
		t.Run(string(status), func(t *testing.T) {
			e := newPayEnv(t)
			orgID := e.newFreeOrg()
			end := epoch.AddDate(0, 0, -5)
			e.setSub(orgID, subSpec{Plan: "pro", Status: status, TrialEndsAt: at(epoch.AddDate(0, -2, 0)), PeriodStart: end.AddDate(0, -1, 0), PeriodEnd: &end})

			res, err := e.svc.Checkout(ownerCtx(orgID), "pro")

			require.NoError(t, err)
			assert.False(t, res.TrialStarted)
			assert.Equal(t, status, e.sub(orgID).Status, "it only comes back once the invoice is paid")
			require.Len(t, e.invoices(orgID), 1)
		})
	}
}

func TestCheckout_RejectsPlansThatCannotBeBought(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	_, err := e.admin.Exec(context.Background(), "UPDATE plans SET is_active = false WHERE code = 'business'")
	require.NoError(t, err)

	for name, plan := range map[string]string{
		"empty":    "",
		"blank":    "   ",
		"unknown":  "platinum",
		"free":     "free",
		"inactive": "business",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := e.svc.Checkout(ownerCtx(orgID), plan)

			require.Error(t, err)
			assert.True(t, apperr.IsCode(err, apperr.CodeValidation), err.Error())
			assert.Equal(t, "plan", firstField(err))
		})
	}
	assert.Empty(t, e.invoices(orgID))
	assert.Equal(t, billing.StatusActive, e.sub(orgID).Status)
}

func firstField(err error) string {
	for field := range apperr.From(err).Details["fields"].(map[string]string) {
		return field
	}
	return ""
}

func TestCheckout_OnlyTheOwnerMayBuy(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()

	for _, role := range []org.Role{org.RoleAdmin, org.RoleMember, ""} {
		t.Run("role "+string(role), func(t *testing.T) {
			_, err := e.svc.Checkout(ctxAs(orgID, role), "pro")

			require.Error(t, err)
			assert.True(t, apperr.IsCode(err, apperr.CodeForbidden), err.Error())
		})
	}
	_, err := e.svc.Checkout(context.Background(), "pro")
	assert.True(t, apperr.IsCode(err, apperr.CodeNotFound), "without a tenant on the context there is nothing to buy for")
	assert.Empty(t, e.invoices(orgID))
	assert.Empty(t, e.gateway.Created())
}

func TestCheckout_AGatewayFailureLeavesNothingBehind(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.gateway.FailWith(errors.New("connection refused"))

	_, err := e.svc.Checkout(ownerCtx(orgID), "pro")

	require.Error(t, err)
	assert.True(t, apperr.IsCode(err, apperr.CodePaymentProvider), err.Error())
	assert.Empty(t, e.invoices(orgID))
	assert.Equal(t, billing.StatusActive, e.sub(orgID).Status, "the trial did not start: the whole checkout rolled back")
	assert.Nil(t, e.sub(orgID).TrialEndsAt)
	assert.Empty(t, e.emails(""), "no email for an invoice that does not exist")

	e.gateway.FailWith(nil)
	res, err := e.svc.Checkout(ownerCtx(orgID), "pro")
	require.NoError(t, err)
	assert.True(t, res.TrialStarted, "and it works once the gateway is back")
	assert.Equal(t, "INV-2026-000002", res.Invoice.Number, "the failed attempt used a number; numbers are never reused")
}

func TestCheckout_AFailureAfterTheGatewayRollsBackTheTrialAndTheInvoice(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	// An email that cannot be queued (the owner's address is invalid) fails after
	// the gateway was called and the invoice stored.
	_, err := e.admin.Exec(context.Background(), "UPDATE users SET email = 'not-an-address' WHERE id = (SELECT owner_id FROM organizations WHERE id = $1)", orgID)
	require.NoError(t, err)

	_, err = e.svc.Checkout(ownerCtx(orgID), "pro")

	require.Error(t, err)
	assert.Empty(t, e.invoices(orgID))
	assert.Equal(t, billing.StatusActive, e.sub(orgID).Status)
	assert.Nil(t, e.sub(orgID).TrialEndsAt)
}

func TestCheckout_ReminderMarksStartSetWhenTheInvoiceIsAlreadyInsideTheReminderWindow(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	e.setTrialDays("pro", 2) // a trial shorter than 3 days: the invoice is created inside the H-3 window

	_, err := e.svc.Checkout(ownerCtx(orgID), "pro")

	require.NoError(t, err)
	inv := e.invoices(orgID)[0]
	require.NotNil(t, inv.Reminded3d, "creating the invoice is itself the notice for the H-3 window")
	assert.True(t, epoch.Equal(*inv.Reminded3d))
	assert.Nil(t, inv.Reminded1d)
}

// --- invoices ---

func TestListInvoices_NewestFirstWithCursorPagination(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	past := epoch.AddDate(0, -3, 0)
	e.setSub(orgID, subSpec{Plan: "free", Status: billing.StatusActive, TrialEndsAt: &past, PeriodStart: past})
	var numbers []string
	for i := 0; i < 5; i++ {
		plan := []string{"pro", "business"}[i%2] // alternating replaces the earlier checkout, one live at a time
		res, err := e.svc.Checkout(ownerCtx(orgID), plan)
		require.NoError(t, err)
		numbers = append(numbers, res.Invoice.Number)
		e.clock.Advance(time.Minute)
	}

	first, err := e.svc.ListInvoices(ownerCtx(orgID), pagination.Params{Limit: 2})
	require.NoError(t, err)
	require.Len(t, first, 3, "one more than the page, to tell there is another page")
	page := pagination.Paginate(first, 2, billing.Invoice.CursorOf)
	require.NotNil(t, page.NextCursor)
	assert.Equal(t, []string{numbers[4], numbers[3]}, []string{page.Items[0].Number, page.Items[1].Number})

	cursor, err := pagination.DecodeCursor(*page.NextCursor)
	require.NoError(t, err)
	second, err := e.svc.ListInvoices(ownerCtx(orgID), pagination.Params{Limit: 2, Cursor: &cursor})
	require.NoError(t, err)
	page2 := pagination.Paginate(second, 2, billing.Invoice.CursorOf)
	assert.Equal(t, []string{numbers[2], numbers[1]}, []string{page2.Items[0].Number, page2.Items[1].Number})
}

func TestListInvoices_PaymentLinksOnlyOnInvoicesThatCanStillBePaid(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	past := epoch.AddDate(0, -3, 0)
	e.setSub(orgID, subSpec{Plan: "free", Status: billing.StatusActive, TrialEndsAt: &past, PeriodStart: past})
	_, err := e.svc.Checkout(ownerCtx(orgID), "pro")
	require.NoError(t, err)
	_, err = e.svc.Checkout(ownerCtx(orgID), "business") // voids the first
	require.NoError(t, err)

	got, err := e.svc.ListInvoices(ownerCtx(orgID), pagination.Params{Limit: 10})

	require.NoError(t, err)
	require.Len(t, got, 2)
	byPlan := map[string]billing.Invoice{got[0].PlanCode: got[0], got[1].PlanCode: got[1]}
	assert.Nil(t, byPlan["pro"].PaymentURL, "void: nothing to pay")
	assert.NotNil(t, byPlan["business"].PaymentURL)
}

func TestInvoices_AnOrganizationNeverSeesAnotherOnes(t *testing.T) {
	e := newPayEnv(t)
	a, b := e.newFreeOrg(), e.newFreeOrg()
	_, err := e.svc.Checkout(ownerCtx(a), "pro")
	require.NoError(t, err)
	resB, err := e.svc.Checkout(ownerCtx(b), "business")
	require.NoError(t, err)

	listA, err := e.svc.ListInvoices(ownerCtx(a), pagination.Params{Limit: 10})
	require.NoError(t, err)
	require.Len(t, listA, 1)
	assert.Equal(t, "pro", listA[0].PlanCode)

	_, err = e.svc.GetInvoice(ownerCtx(a), resB.Invoice.ID)
	assert.True(t, apperr.IsCode(err, apperr.CodeNotFound), "B's invoice by id inside A's tenant")
	_, err = e.svc.GetInvoice(ownerCtx(a), uuid.New())
	assert.True(t, apperr.IsCode(err, apperr.CodeNotFound))
}

func TestInvoices_OnlyTheOwnerMayReadThem(t *testing.T) {
	e := newPayEnv(t)
	orgID := e.newFreeOrg()
	res, err := e.svc.Checkout(ownerCtx(orgID), "pro")
	require.NoError(t, err)

	for _, role := range []org.Role{org.RoleAdmin, org.RoleMember} {
		_, err := e.svc.ListInvoices(ctxAs(orgID, role), pagination.Params{Limit: 10})
		assert.True(t, apperr.IsCode(err, apperr.CodeForbidden), "%s lists", role)
		_, err = e.svc.GetInvoice(ctxAs(orgID, role), res.Invoice.ID)
		assert.True(t, apperr.IsCode(err, apperr.CodeForbidden), "%s reads", role)
	}
}

// Row-level security: the application connection sees only the invoices of
// the tenant it is pinned to, whatever the query asks for.
func TestInvoices_RowLevelSecurityHidesOtherTenantsFromAQueryWithoutAFilter(t *testing.T) {
	e := newPayEnv(t)
	a, b := e.newFreeOrg(), e.newFreeOrg()
	_, err := e.svc.Checkout(ownerCtx(a), "pro")
	require.NoError(t, err)
	_, err = e.svc.Checkout(ownerCtx(b), "pro")
	require.NoError(t, err)
	require.Equal(t, 2, e.countRows("SELECT count(*) FROM invoices"), "the owner connection sees both")

	var withoutTenant, pinned int
	require.NoError(t, e.db.Pool().QueryRow(context.Background(), "SELECT count(*) FROM invoices").Scan(&withoutTenant))
	e.inTenant(a, func(ctx context.Context, tx pgx.Tx) {
		require.NoError(t, tx.QueryRow(ctx, "SELECT count(*) FROM invoices").Scan(&pinned))
	})

	assert.Zero(t, withoutTenant, "no tenant, no rows")
	assert.Equal(t, 1, pinned, "a query with no WHERE still only sees its own tenant")
}
