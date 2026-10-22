package billing_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/billing"
)

var (
	t0      = time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	freeID  = uuid.MustParse("00000000-0000-7000-8000-000000000001")
	proID   = uuid.MustParse("00000000-0000-7000-8000-000000000002")
	bizID   = uuid.MustParse("00000000-0000-7000-8000-000000000003")
	grace   = 3 * 24 * time.Hour
	oneHour = time.Hour
)

var (
	allStatuses = []billing.Status{billing.StatusTrialing, billing.StatusActive, billing.StatusPastDue, billing.StatusExpired, billing.StatusCanceled}
	allKinds    = []billing.EventKind{
		billing.EventTrialStarted, billing.EventInvoicePaid, billing.EventTrialEnded,
		billing.EventPeriodEnded, billing.EventGraceEnded, billing.EventOrganizationDeleted,
	}
)

func ptr[T any](v T) *T { return &v }

// freeSub is an organization on Free: active, no period end, no trial yet.
func freeSub() billing.Snapshot {
	return billing.Snapshot{Status: billing.StatusActive, PlanID: freeID, CurrentPeriodStart: t0.AddDate(0, -1, 0)}
}

// paidSub is active on Pro with the period ending at end.
func paidSub(end time.Time) billing.Snapshot {
	return billing.Snapshot{
		Status: billing.StatusActive, PlanID: proID,
		CurrentPeriodStart: end.AddDate(0, -1, 0), CurrentPeriodEnd: &end,
	}
}

func trialingSub(end time.Time) billing.Snapshot {
	return billing.Snapshot{
		Status: billing.StatusTrialing, PlanID: proID, TrialEndsAt: &end,
		CurrentPeriodStart: end.AddDate(0, 0, -14), CurrentPeriodEnd: &end,
	}
}

func pastDueSub(periodEnd time.Time) billing.Snapshot {
	s := paidSub(periodEnd)
	s.Status = billing.StatusPastDue
	until := periodEnd.Add(grace)
	s.GraceUntil = &until
	return s
}

func checkoutInvoice(plan uuid.UUID) *billing.PaidInvoice {
	return &billing.PaidInvoice{Kind: billing.InvoiceKindCheckout, PlanID: plan, Interval: "month"}
}

// renewalFor is the paid renewal for the period that follows the one ending at end.
func renewalFor(end time.Time, plan uuid.UUID) *billing.PaidInvoice {
	return &billing.PaidInvoice{Kind: billing.InvoiceKindRenewal, PlanID: plan, Interval: "month", PeriodStart: end, PeriodEnd: end.AddDate(0, 1, 0)}
}

func next(t *testing.T, s billing.Snapshot, e billing.Event, now time.Time) billing.Result {
	t.Helper()
	res, err := billing.Next(s, e, now)
	require.NoError(t, err)
	return res
}

// representative returns a snapshot of the status and an event of the kind
// that are valid for each other apart from the transition table itself, so the
// table is the only thing that can refuse them.
func representative(status billing.Status, kind billing.EventKind) (billing.Snapshot, billing.Event) {
	var s billing.Snapshot
	switch status {
	case billing.StatusTrialing:
		s = trialingSub(t0.Add(-oneHour))
	case billing.StatusActive:
		s = paidSub(t0.Add(-oneHour))
		if kind == billing.EventTrialStarted {
			s = freeSub()
		}
	case billing.StatusPastDue:
		s = pastDueSub(t0.Add(-grace - oneHour))
	case billing.StatusExpired, billing.StatusCanceled:
		s = paidSub(t0.Add(-oneHour))
		s.Status = status
	}
	e := billing.Event{Kind: kind, Grace: grace, Invoice: checkoutInvoice(bizID), Trial: &billing.Trial{PlanID: proID, Days: 14}}
	return s, e
}

// Every (status, event) pair is either in the transition table or rejected,
// and a permitted one always ends in a status the table lists for it.
func TestNext_TheTransitionTableIsExhaustive(t *testing.T) {
	expected := map[billing.Status]map[billing.EventKind][]billing.Status{
		billing.StatusTrialing: {
			billing.EventInvoicePaid:         {billing.StatusActive},
			billing.EventTrialEnded:          {billing.StatusExpired},
			billing.EventOrganizationDeleted: {billing.StatusCanceled},
		},
		billing.StatusActive: {
			billing.EventTrialStarted:        {billing.StatusTrialing},
			billing.EventInvoicePaid:         {billing.StatusActive},
			billing.EventPeriodEnded:         {billing.StatusPastDue, billing.StatusActive, billing.StatusCanceled},
			billing.EventOrganizationDeleted: {billing.StatusCanceled},
		},
		billing.StatusPastDue: {
			billing.EventInvoicePaid:         {billing.StatusActive},
			billing.EventGraceEnded:          {billing.StatusExpired},
			billing.EventOrganizationDeleted: {billing.StatusCanceled},
		},
		billing.StatusExpired: {
			billing.EventInvoicePaid:         {billing.StatusActive},
			billing.EventOrganizationDeleted: {billing.StatusCanceled},
		},
		billing.StatusCanceled: {
			billing.EventInvoicePaid: {billing.StatusActive},
		},
	}
	for _, status := range allStatuses {
		for _, kind := range allKinds {
			t.Run(string(status)+"/"+string(kind), func(t *testing.T) {
				s, e := representative(status, kind)
				allowed := expected[status][kind]
				assert.Equal(t, allowed, billing.AllowedTransitions(status, kind), "the exported table matches the documented one")

				res, err := billing.Next(s, e, t0)

				if len(allowed) == 0 {
					assert.ErrorIs(t, err, billing.ErrIllegalTransition)
					return
				}
				require.NoError(t, err)
				assert.Contains(t, allowed, res.Snapshot.Status)
			})
		}
	}
}

func TestNext_TheTableIsCoveredByTheEnumerations(t *testing.T) {
	// If a status or an event is added, the exhaustive test above must list it.
	count := 0
	for _, s := range allStatuses {
		for _, k := range allKinds {
			if len(billing.AllowedTransitions(s, k)) > 0 {
				count++
			}
		}
	}
	assert.Equal(t, 13, count, "transitions in the table; update the exhaustive test when this changes")
	assert.Empty(t, billing.AllowedTransitions("paused", billing.EventInvoicePaid), "an unknown status accepts nothing")
	assert.Empty(t, billing.AllowedTransitions(billing.StatusActive, "refunded"), "an unknown event is accepted by nothing")
}

func TestNext_UnknownEventsAndStatusesAreIllegal(t *testing.T) {
	_, err := billing.Next(paidSub(t0), billing.Event{Kind: "refunded"}, t0)
	assert.ErrorIs(t, err, billing.ErrIllegalTransition)

	s := paidSub(t0.Add(-oneHour))
	s.Status = "paused"
	_, err = billing.Next(s, billing.Event{Kind: billing.EventPeriodEnded, Grace: grace}, t0)
	assert.ErrorIs(t, err, billing.ErrIllegalTransition)
}

// --- free -> trialing ---

func TestNext_AFreeOrganizationStartsATrialOnTheChosenPlan(t *testing.T) {
	res := next(t, freeSub(), billing.Event{Kind: billing.EventTrialStarted, Trial: &billing.Trial{PlanID: proID, Days: 14}}, t0)

	assert.Equal(t, billing.OutcomeTrialStarted, res.Outcome)
	assert.True(t, res.Changed)
	got := res.Snapshot
	assert.Equal(t, billing.StatusTrialing, got.Status)
	assert.Equal(t, proID, got.PlanID)
	require.NotNil(t, got.TrialEndsAt)
	assert.Equal(t, t0.AddDate(0, 0, 14), *got.TrialEndsAt)
	assert.Equal(t, t0, got.CurrentPeriodStart)
	require.NotNil(t, got.CurrentPeriodEnd)
	assert.Equal(t, *got.TrialEndsAt, *got.CurrentPeriodEnd, "the period of a trial is the trial")
}

func TestNext_ATrialIsOnlyForOrganizationsThatNeverPaidOrTried(t *testing.T) {
	trialEvent := billing.Event{Kind: billing.EventTrialStarted, Trial: &billing.Trial{PlanID: proID, Days: 14}}
	used := freeSub()
	used.TrialEndsAt = ptr(t0.AddDate(0, -3, 0)) // trialed long ago and fell back to Free
	paying := paidSub(t0.AddDate(0, 0, 10))      // an active, paying subscription

	_, err := billing.Next(used, trialEvent, t0)
	assert.ErrorIs(t, err, billing.ErrNotEligible)
	_, err = billing.Next(paying, trialEvent, t0)
	assert.ErrorIs(t, err, billing.ErrNotEligible)
}

func TestNext_ATrialNeedsAPlanAndDays(t *testing.T) {
	for name, e := range map[string]billing.Event{
		"no trial":   {Kind: billing.EventTrialStarted},
		"zero days":  {Kind: billing.EventTrialStarted, Trial: &billing.Trial{PlanID: proID}},
		"never days": {Kind: billing.EventTrialStarted, Trial: &billing.Trial{PlanID: proID, Days: -1}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := billing.Next(freeSub(), e, t0)

			assert.ErrorIs(t, err, billing.ErrInvalidEvent)
		})
	}
}

// --- trialing ---

func TestNext_APaidCheckoutTurnsATrialIntoAnActiveSubscriptionStartingNow(t *testing.T) {
	trialEnd := t0.AddDate(0, 0, 9)
	s := trialingSub(trialEnd)

	res := next(t, s, billing.Event{Kind: billing.EventInvoicePaid, Invoice: checkoutInvoice(proID)}, t0)

	assert.Equal(t, billing.OutcomeActivated, res.Outcome)
	got := res.Snapshot
	assert.Equal(t, billing.StatusActive, got.Status)
	assert.Equal(t, t0, got.CurrentPeriodStart, "the paid period starts when it is paid (spec §8.4)")
	assert.Equal(t, t0.AddDate(0, 1, 0), *got.CurrentPeriodEnd)
	assert.Equal(t, trialEnd, *got.TrialEndsAt, "the trial stays marked as used")
}

func TestNext_AnUnpaidTrialExpiresWhenItEnds(t *testing.T) {
	trialEnd := t0
	s := trialingSub(trialEnd)

	_, err := billing.Next(s, billing.Event{Kind: billing.EventTrialEnded}, trialEnd.Add(-time.Nanosecond))
	assert.ErrorIs(t, err, billing.ErrNotDue, "one instant early")

	res := next(t, s, billing.Event{Kind: billing.EventTrialEnded}, trialEnd)
	assert.Equal(t, billing.StatusExpired, res.Snapshot.Status, "exactly at the end")
	assert.Equal(t, billing.OutcomeExpired, res.Outcome)
}

func TestNext_ARenewalCannotActivateATrial(t *testing.T) {
	s := trialingSub(t0.AddDate(0, 0, 9))

	_, err := billing.Next(s, billing.Event{Kind: billing.EventInvoicePaid, Invoice: renewalFor(*s.CurrentPeriodEnd, proID)}, t0)

	assert.ErrorIs(t, err, billing.ErrStaleInvoice)
}

// --- active ---

func TestNext_AnUpgradePaidWhileActiveStartsANewPeriodOnTheNewPlan(t *testing.T) {
	s := paidSub(t0.AddDate(0, 0, 10))
	s.CancelAtPeriodEnd = true
	s.PendingPlanID = ptr(freeID)

	res := next(t, s, billing.Event{Kind: billing.EventInvoicePaid, Invoice: checkoutInvoice(bizID)}, t0)

	got := res.Snapshot
	assert.Equal(t, billing.OutcomeActivated, res.Outcome)
	assert.Equal(t, billing.StatusActive, got.Status)
	assert.Equal(t, bizID, got.PlanID)
	assert.Equal(t, t0, got.CurrentPeriodStart, "the old period is not credited and the new one starts now")
	assert.Equal(t, t0.AddDate(0, 1, 0), *got.CurrentPeriodEnd)
	assert.False(t, got.CancelAtPeriodEnd, "buying a plan ends a pending cancellation")
	assert.Nil(t, got.PendingPlanID, "and a pending downgrade")
}

func TestNext_AFreeOrganizationThatPaysIsActiveOnTheNewPlanWithAPeriod(t *testing.T) {
	res := next(t, freeSub(), billing.Event{Kind: billing.EventInvoicePaid, Invoice: checkoutInvoice(proID)}, t0)

	assert.Equal(t, proID, res.Snapshot.PlanID)
	require.NotNil(t, res.Snapshot.CurrentPeriodEnd, "it is billed from now on")
}

func TestNext_ARenewalPaidBeforeThePeriodEndsOnlyRecordsThePayment(t *testing.T) {
	end := t0.AddDate(0, 0, 2)
	s := paidSub(end)
	s.PendingPlanID = ptr(bizID)

	res := next(t, s, billing.Event{Kind: billing.EventInvoicePaid, Invoice: renewalFor(end, bizID)}, t0)

	assert.Equal(t, billing.OutcomePaidAhead, res.Outcome)
	assert.False(t, res.Changed)
	assert.Equal(t, s, res.Snapshot, "the new plan waits for the period end")
}

func TestNext_ARenewalForAnotherPeriodIsStale(t *testing.T) {
	end := t0.AddDate(0, 0, 2)
	s := paidSub(end)

	for name, inv := range map[string]*billing.PaidInvoice{
		"an older period":     renewalFor(end.AddDate(0, -1, 0), proID),
		"a period after next": renewalFor(end.AddDate(0, 1, 0), proID),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := billing.Next(s, billing.Event{Kind: billing.EventInvoicePaid, Invoice: inv}, t0)

			assert.ErrorIs(t, err, billing.ErrStaleInvoice)
		})
	}
	free := freeSub()
	_, err := billing.Next(free, billing.Event{Kind: billing.EventInvoicePaid, Invoice: renewalFor(t0, proID)}, t0)
	assert.ErrorIs(t, err, billing.ErrStaleInvoice, "a Free subscription is not renewing")
}

func TestNext_AnUnpaidPeriodEndStartsTheGracePeriodFromThePeriodEnd(t *testing.T) {
	end := t0
	s := paidSub(end)

	_, err := billing.Next(s, billing.Event{Kind: billing.EventPeriodEnded, Grace: grace}, end.Add(-time.Nanosecond))
	assert.ErrorIs(t, err, billing.ErrNotDue, "one instant early")

	// The job notices 20 minutes late; the grace still counts from the period end.
	res := next(t, s, billing.Event{Kind: billing.EventPeriodEnded, Grace: grace}, end.Add(20*time.Minute))
	assert.Equal(t, billing.OutcomePastDue, res.Outcome)
	assert.Equal(t, billing.StatusPastDue, res.Snapshot.Status)
	assert.Equal(t, end.Add(grace), *res.Snapshot.GraceUntil)
	assert.Equal(t, proID, res.Snapshot.PlanID, "access to the plan continues during the grace period")
}

func TestNext_APaidNextPeriodIsRolledIntoAndConsumesAMatchingDowngrade(t *testing.T) {
	end := t0
	s := paidSub(end)
	s.PendingPlanID = ptr(freeID)
	other := paidSub(end)
	other.PendingPlanID = ptr(bizID)
	paid := renewalFor(end, proID)

	res := next(t, s, billing.Event{Kind: billing.EventPeriodEnded, NextPeriod: paid, Grace: grace}, end)
	assert.Equal(t, billing.OutcomeRenewed, res.Outcome)
	assert.Equal(t, billing.StatusActive, res.Snapshot.Status)
	assert.Equal(t, end, res.Snapshot.CurrentPeriodStart)
	assert.Equal(t, end.AddDate(0, 1, 0), *res.Snapshot.CurrentPeriodEnd)
	assert.Equal(t, freeID, *res.Snapshot.PendingPlanID, "a downgrade for another plan than the one billed is kept")

	// A renewal billed for the pending plan applies it.
	pending := paidSub(end)
	pending.PendingPlanID = ptr(bizID)
	res = next(t, pending, billing.Event{Kind: billing.EventPeriodEnded, NextPeriod: renewalFor(end, bizID), Grace: grace}, end)
	assert.Equal(t, bizID, res.Snapshot.PlanID)
	assert.Nil(t, res.Snapshot.PendingPlanID)
}

func TestNext_ARenewalBilledForAnotherPeriodIsStaleAtThePeriodEnd(t *testing.T) {
	end := t0
	_, err := billing.Next(paidSub(end), billing.Event{Kind: billing.EventPeriodEnded, NextPeriod: renewalFor(end.AddDate(0, 1, 0), proID), Grace: grace}, end)

	assert.ErrorIs(t, err, billing.ErrStaleInvoice)
}

func TestNext_ACancelingSubscriptionIsCanceledAtThePeriodEndWhateverElseIsPaid(t *testing.T) {
	end := t0
	s := paidSub(end)
	s.CancelAtPeriodEnd = true
	s.PendingPlanID = ptr(freeID)

	res := next(t, s, billing.Event{Kind: billing.EventPeriodEnded, NextPeriod: renewalFor(end, proID), Grace: grace}, end)

	assert.Equal(t, billing.OutcomeCanceled, res.Outcome)
	assert.Equal(t, billing.StatusCanceled, res.Snapshot.Status)
	assert.Nil(t, res.Snapshot.PendingPlanID)
	assert.Equal(t, proID, res.Snapshot.PlanID, "the history of what was subscribed is kept")
}

func TestNext_AFreeSubscriptionNeverTransitionsOnItsOwn(t *testing.T) {
	free := freeSub()
	far := t0.AddDate(50, 0, 0)

	_, err := billing.Next(free, billing.Event{Kind: billing.EventPeriodEnded, Grace: grace}, far)
	assert.ErrorIs(t, err, billing.ErrNotDue)
	_, due := billing.DueEvent(free, nil, grace, far)
	assert.False(t, due)
}

// --- past_due ---

func TestNext_APastDueSubscriptionPaysItsRenewalAndContinuesFromTheOldPeriodEnd(t *testing.T) {
	end := t0.Add(-oneHour)
	s := pastDueSub(end)
	s.PendingPlanID = ptr(bizID)

	res := next(t, s, billing.Event{Kind: billing.EventInvoicePaid, Invoice: renewalFor(end, bizID)}, t0)

	got := res.Snapshot
	assert.Equal(t, billing.OutcomeRenewed, res.Outcome)
	assert.Equal(t, billing.StatusActive, got.Status)
	assert.Nil(t, got.GraceUntil)
	assert.Equal(t, bizID, got.PlanID)
	assert.Nil(t, got.PendingPlanID)
	assert.Equal(t, end, got.CurrentPeriodStart, "no gap and no free days: the next period follows the last")
	assert.Equal(t, end.AddDate(0, 1, 0), *got.CurrentPeriodEnd)
}

func TestNext_APastDueSubscriptionPayingAFreshCheckoutStartsFromNow(t *testing.T) {
	res := next(t, pastDueSub(t0.Add(-oneHour)), billing.Event{Kind: billing.EventInvoicePaid, Invoice: checkoutInvoice(proID)}, t0)

	assert.Equal(t, billing.OutcomeActivated, res.Outcome)
	assert.Equal(t, t0, res.Snapshot.CurrentPeriodStart)
	assert.Nil(t, res.Snapshot.GraceUntil)
}

func TestNext_APastDueSubscriptionExpiresWhenTheGraceEnds(t *testing.T) {
	s := pastDueSub(t0.Add(-grace))
	require.Equal(t, t0, *s.GraceUntil)

	_, err := billing.Next(s, billing.Event{Kind: billing.EventGraceEnded}, t0.Add(-time.Nanosecond))
	assert.ErrorIs(t, err, billing.ErrNotDue)

	res := next(t, s, billing.Event{Kind: billing.EventGraceEnded}, t0)
	assert.Equal(t, billing.StatusExpired, res.Snapshot.Status)
	assert.Equal(t, billing.OutcomeExpired, res.Outcome)
}

func TestNext_ARenewalForAnOldPeriodIsStaleOnAPastDueSubscription(t *testing.T) {
	end := t0.Add(-oneHour)

	_, err := billing.Next(pastDueSub(end), billing.Event{Kind: billing.EventInvoicePaid, Invoice: renewalFor(end.AddDate(0, 1, 0), proID)}, t0)

	assert.ErrorIs(t, err, billing.ErrStaleInvoice)
}

// --- expired / canceled ---

func TestNext_AnExpiredOrCanceledOrganizationSubscribesAgainByPayingACheckout(t *testing.T) {
	for _, status := range []billing.Status{billing.StatusExpired, billing.StatusCanceled} {
		t.Run(string(status), func(t *testing.T) {
			s := paidSub(t0.AddDate(0, 0, -20))
			s.Status = status

			res := next(t, s, billing.Event{Kind: billing.EventInvoicePaid, Invoice: checkoutInvoice(bizID)}, t0)

			assert.Equal(t, billing.OutcomeActivated, res.Outcome)
			assert.Equal(t, billing.StatusActive, res.Snapshot.Status)
			assert.Equal(t, bizID, res.Snapshot.PlanID)
			assert.Equal(t, t0, res.Snapshot.CurrentPeriodStart)
		})
	}
}

func TestNext_ARenewalPaidAfterTheSubscriptionEndedIsStale(t *testing.T) {
	for _, status := range []billing.Status{billing.StatusExpired, billing.StatusCanceled} {
		t.Run(string(status), func(t *testing.T) {
			end := t0.AddDate(0, 0, -5)
			s := paidSub(end)
			s.Status = status

			_, err := billing.Next(s, billing.Event{Kind: billing.EventInvoicePaid, Invoice: renewalFor(end, proID)}, t0)

			assert.ErrorIs(t, err, billing.ErrStaleInvoice, "the money came in; a person has to decide what to do with it")
		})
	}
}

// --- organization deleted ---

func TestNext_ADeletedOrganizationIsCanceledFromEveryStatusExceptWhenAlreadyCanceled(t *testing.T) {
	for _, status := range []billing.Status{billing.StatusTrialing, billing.StatusActive, billing.StatusPastDue, billing.StatusExpired} {
		t.Run(string(status), func(t *testing.T) {
			s, _ := representative(status, billing.EventOrganizationDeleted)
			s.CancelAtPeriodEnd = true
			s.PendingPlanID = ptr(freeID)

			res := next(t, s, billing.Event{Kind: billing.EventOrganizationDeleted}, t0)

			assert.Equal(t, billing.StatusCanceled, res.Snapshot.Status)
			assert.Equal(t, billing.OutcomeCanceled, res.Outcome)
			assert.Nil(t, res.Snapshot.PendingPlanID)
			assert.Nil(t, res.Snapshot.GraceUntil)
		})
	}
	s, _ := representative(billing.StatusCanceled, billing.EventOrganizationDeleted)
	_, err := billing.Next(s, billing.Event{Kind: billing.EventOrganizationDeleted}, t0)
	assert.ErrorIs(t, err, billing.ErrIllegalTransition, "already canceled: the caller treats it as done")
}

// --- invalid events ---

func TestNext_APaymentNeedsAnInvoiceOfAKnownKindAndInterval(t *testing.T) {
	_, err := billing.Next(paidSub(t0), billing.Event{Kind: billing.EventInvoicePaid}, t0)
	assert.ErrorIs(t, err, billing.ErrInvalidEvent)

	_, err = billing.Next(paidSub(t0), billing.Event{Kind: billing.EventInvoicePaid, Invoice: &billing.PaidInvoice{Kind: "gift", PlanID: proID}}, t0)
	assert.ErrorIs(t, err, billing.ErrInvalidEvent)

	_, err = billing.Next(paidSub(t0), billing.Event{Kind: billing.EventInvoicePaid, Invoice: &billing.PaidInvoice{Kind: billing.InvoiceKindCheckout, PlanID: proID, Interval: "week"}}, t0)
	assert.ErrorIs(t, err, billing.ErrInvalidEvent)
}

// --- purity and idempotency ---

func TestNext_DoesNotModifyItsInput(t *testing.T) {
	end := t0
	s := paidSub(end)
	s.PendingPlanID = ptr(bizID)
	before := s
	pendingBefore := *s.PendingPlanID
	endBefore := *s.CurrentPeriodEnd

	_ = next(t, s, billing.Event{Kind: billing.EventPeriodEnded, NextPeriod: renewalFor(end, bizID), Grace: grace}, end)

	assert.Equal(t, before, s)
	assert.Equal(t, pendingBefore, *s.PendingPlanID)
	assert.Equal(t, endBefore, *s.CurrentPeriodEnd)
}

// Applying the due event of a snapshot to its own result is refused and finds
// nothing due, so a job that runs twice changes nothing the second time.
func TestNext_TimeDrivenEventsAreNotAppliedTwice(t *testing.T) {
	cases := map[string]billing.Snapshot{
		"trial ended":  trialingSub(t0),
		"period ended": paidSub(t0),
		"grace ended":  pastDueSub(t0.Add(-grace)),
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			event, due := billing.DueEvent(s, nil, grace, t0)
			require.True(t, due)
			first := next(t, s, event, t0)

			_, stillDue := billing.DueEvent(first.Snapshot, nil, grace, t0)
			_, err := billing.Next(first.Snapshot, event, t0)

			assert.False(t, stillDue)
			assert.Error(t, err)
		})
	}
}

func TestDueEvent_PicksTheEventOfTheStatusOnceItsTimeHasCome(t *testing.T) {
	paid := renewalFor(t0, proID)
	tests := []struct {
		name string
		s    billing.Snapshot
		now  time.Time
		want billing.EventKind
	}{
		{"trial not over", trialingSub(t0), t0.Add(-time.Nanosecond), ""},
		{"trial over", trialingSub(t0), t0, billing.EventTrialEnded},
		{"period not over", paidSub(t0), t0.Add(-time.Nanosecond), ""},
		{"period over", paidSub(t0), t0, billing.EventPeriodEnded},
		{"grace not over", pastDueSub(t0.Add(-grace)), t0.Add(-time.Nanosecond), ""},
		{"grace over", pastDueSub(t0.Add(-grace)), t0, billing.EventGraceEnded},
		{"free", freeSub(), t0.AddDate(10, 0, 0), ""},
		{"expired", func() billing.Snapshot { s := paidSub(t0); s.Status = billing.StatusExpired; return s }(), t0.AddDate(1, 0, 0), ""},
		{"canceled", func() billing.Snapshot { s := paidSub(t0); s.Status = billing.StatusCanceled; return s }(), t0.AddDate(1, 0, 0), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, due := billing.DueEvent(tt.s, paid, grace, tt.now)

			assert.Equal(t, tt.want != "", due)
			assert.Equal(t, tt.want, e.Kind)
			if tt.want == billing.EventPeriodEnded {
				assert.Equal(t, paid, e.NextPeriod)
				assert.Equal(t, grace, e.Grace)
			}
		})
	}
}

func TestAddInterval(t *testing.T) {
	d := func(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 9, 30, 0, 0, time.UTC) }
	tests := []struct {
		name     string
		from     time.Time
		interval string
		want     time.Time
	}{
		{"plain month", d(2026, 6, 1), "month", d(2026, 7, 1)},
		{"31st into a shorter month", d(2026, 1, 31), "month", d(2026, 2, 28)},
		{"31st into a leap February", d(2028, 1, 31), "month", d(2028, 2, 29)},
		{"31st into a 30 day month", d(2026, 3, 31), "month", d(2026, 4, 30)},
		{"december into january", d(2026, 12, 15), "month", d(2027, 1, 15)},
		{"december 31st", d(2026, 12, 31), "month", d(2027, 1, 31)},
		{"plain year", d(2026, 6, 1), "year", d(2027, 6, 1)},
		{"leap day into a common year", d(2028, 2, 29), "year", d(2029, 2, 28)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := billing.AddInterval(tt.from, tt.interval)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
	_, err := billing.AddInterval(t0, "week")
	assert.Error(t, err)
}

// --- cancel, resume and downgrade rules ---

func TestRequestCancellation_OnlyForARunningPaidSubscription(t *testing.T) {
	for _, status := range []billing.Status{billing.StatusTrialing, billing.StatusActive, billing.StatusPastDue} {
		t.Run(string(status), func(t *testing.T) {
			s, _ := representative(status, billing.EventOrganizationDeleted)

			got, err := billing.RequestCancellation(s)

			require.NoError(t, err)
			assert.True(t, got.CancelAtPeriodEnd)
			assert.Equal(t, status, got.Status, "access continues until the period ends")
			again, err := billing.RequestCancellation(got)
			require.NoError(t, err)
			assert.Equal(t, got, again, "asking twice changes nothing")
		})
	}
	for name, s := range map[string]billing.Snapshot{
		"free": freeSub(),
		"expired": func() billing.Snapshot {
			s, _ := representative(billing.StatusExpired, billing.EventOrganizationDeleted)
			return s
		}(),
		"canceled": func() billing.Snapshot {
			s, _ := representative(billing.StatusCanceled, billing.EventOrganizationDeleted)
			return s
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := billing.RequestCancellation(s)
			assert.ErrorIs(t, err, billing.ErrNotRenewing)
			_, err = billing.RevokeCancellation(s)
			assert.ErrorIs(t, err, billing.ErrNotRenewing)
		})
	}
}

func TestRevokeCancellation_TakesTheCancellationBack(t *testing.T) {
	s := paidSub(t0.AddDate(0, 0, 5))
	s.CancelAtPeriodEnd = true

	got, err := billing.RevokeCancellation(s)

	require.NoError(t, err)
	assert.False(t, got.CancelAtPeriodEnd)
	again, err := billing.RevokeCancellation(got)
	require.NoError(t, err)
	assert.Equal(t, got, again)
}

func TestScheduleDowngrade_OnlyForAnActivePaidSubscriptionAndAnotherPlan(t *testing.T) {
	active := paidSub(t0.AddDate(0, 0, 5))

	got, err := billing.ScheduleDowngrade(active, freeID)
	require.NoError(t, err)
	require.NotNil(t, got.PendingPlanID)
	assert.Equal(t, freeID, *got.PendingPlanID)
	assert.Equal(t, proID, got.PlanID, "nothing changes now")

	_, err = billing.ScheduleDowngrade(active, proID)
	assert.ErrorIs(t, err, billing.ErrInvalidEvent, "the plan it is already on")
	for name, s := range map[string]billing.Snapshot{
		"trialing": trialingSub(t0.AddDate(0, 0, 5)), "past_due": pastDueSub(t0), "free": freeSub(),
	} {
		_, err := billing.ScheduleDowngrade(s, freeID)
		assert.ErrorIs(t, err, billing.ErrNotRenewing, name)
	}
	assert.Nil(t, billing.CancelDowngrade(got).PendingPlanID)
}
