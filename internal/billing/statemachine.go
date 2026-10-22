package billing

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// The subscription state machine (spec §8.2) is a set of pure functions: they
// take a snapshot of the subscription, an event and the current time, and
// return the next snapshot. They touch no database and no clock, so every
// transition is tested with plain values and a chosen "now"; the code that
// persists the result (subscription_lifecycle.go) only applies what they say.
//
//	trialing --paid--> active          trialing --trial over, unpaid--> expired
//	active --period over, renewal unpaid--> past_due --paid--> active
//	past_due --grace over--> expired   active --cancel at period end--> canceled
//	expired / canceled --paid (subscribe again)--> active
//	free (active, no period end) --checkout of a plan with a trial--> trialing
//	any --organization deleted--> canceled
//
// Time drives some of them (trial end, period end, grace end): the cron jobs
// ask DueEvent what is due and feed it back through Next.

// EventKind is something that can happen to a subscription.
type EventKind string

const (
	// EventTrialStarted: a Free organization picked a plan with a trial.
	EventTrialStarted EventKind = "trial_started"
	// EventInvoicePaid: the gateway confirmed payment of an invoice.
	EventInvoicePaid EventKind = "invoice_paid"
	// EventTrialEnded: the trial ran out (only valid at or after trial_ends_at).
	EventTrialEnded EventKind = "trial_ended"
	// EventPeriodEnded: the paid period ran out (only at or after its end).
	EventPeriodEnded EventKind = "period_ended"
	// EventGraceEnded: the grace period ran out (only at or after grace_until).
	EventGraceEnded EventKind = "grace_ended"
	// EventOrganizationDeleted: the organization was deleted.
	EventOrganizationDeleted EventKind = "organization_deleted"
)

// transitions is the transition table: for each status, the events it accepts
// and the statuses each may lead to (several when the data decides, e.g. a
// period that ends can lead to past_due, a renewal, or canceled). Anything not
// listed is an illegal transition and Next refuses it.
var transitions = map[Status]map[EventKind][]Status{
	StatusTrialing: {
		EventInvoicePaid:         {StatusActive},
		EventTrialEnded:          {StatusExpired},
		EventOrganizationDeleted: {StatusCanceled},
	},
	StatusActive: {
		EventTrialStarted:        {StatusTrialing},
		EventInvoicePaid:         {StatusActive},
		EventPeriodEnded:         {StatusPastDue, StatusActive, StatusCanceled},
		EventOrganizationDeleted: {StatusCanceled},
	},
	StatusPastDue: {
		EventInvoicePaid:         {StatusActive},
		EventGraceEnded:          {StatusExpired},
		EventOrganizationDeleted: {StatusCanceled},
	},
	StatusExpired: {
		EventInvoicePaid:         {StatusActive},
		EventOrganizationDeleted: {StatusCanceled},
	},
	StatusCanceled: {
		EventInvoicePaid: {StatusActive},
	},
}

// AllowedTransitions returns the statuses an event may lead to from a status,
// or nil when the event is illegal there.
func AllowedTransitions(from Status, event EventKind) []Status {
	return append([]Status(nil), transitions[from][event]...)
}

// Errors of Next.
var (
	// ErrIllegalTransition: the status does not accept the event at all.
	ErrIllegalTransition = errors.New("billing: illegal subscription transition")
	// ErrNotDue: a time-driven event arrived before its time (or the
	// subscription has no such time, as a Free one has no period end).
	ErrNotDue = errors.New("billing: subscription event is not due yet")
	// ErrStaleInvoice: the paid invoice no longer fits the subscription (a
	// renewal for a period that is not the next one, or for a subscription that
	// is no longer renewing). The money was received; a person must look.
	ErrStaleInvoice = errors.New("billing: paid invoice does not fit the subscription")
	// ErrNotEligible: the organization cannot start a trial.
	ErrNotEligible = errors.New("billing: organization is not eligible for a trial")
	// ErrInvalidEvent: the event is missing the data its kind needs.
	ErrInvalidEvent = errors.New("billing: invalid subscription event")
)

// Snapshot is the part of a subscription the transition rules read and change.
type Snapshot struct {
	Status Status
	PlanID uuid.UUID
	// TrialEndsAt is set once a trial started and stays set: it marks the trial
	// as used.
	TrialEndsAt        *time.Time
	CurrentPeriodStart time.Time
	// CurrentPeriodEnd is nil for Free, which never ends and never transitions.
	CurrentPeriodEnd  *time.Time
	CancelAtPeriodEnd bool
	PendingPlanID     *uuid.UUID
	GraceUntil        *time.Time
}

// InvoiceKind says what a paid invoice buys (invoices.kind).
type InvoiceKind string

const (
	// InvoiceKindCheckout is a purchase the organization asked for; the paid
	// period starts when the invoice is paid.
	InvoiceKindCheckout InvoiceKind = "checkout"
	// InvoiceKindRenewal is the next period of a running subscription.
	InvoiceKindRenewal InvoiceKind = "renewal"
)

// PaidInvoice is what a transition needs to know about an invoice that was paid.
type PaidInvoice struct {
	Kind   InvoiceKind
	PlanID uuid.UUID
	// Interval is the plan's billing interval ("month" or "year"), used to
	// length the period of a checkout.
	Interval string
	// PeriodStart and PeriodEnd are the period a renewal bills.
	PeriodStart, PeriodEnd time.Time
}

// Trial describes the trial of EventTrialStarted.
type Trial struct {
	PlanID uuid.UUID
	Days   int
}

// Event is an input to Next. Which fields matter depends on Kind.
type Event struct {
	Kind EventKind
	// Invoice is the paid invoice (EventInvoicePaid).
	Invoice *PaidInvoice
	// Trial is the plan and length of the trial (EventTrialStarted).
	Trial *Trial
	// NextPeriod is the renewal for the period after the current one, when it
	// is already paid (EventPeriodEnded).
	NextPeriod *PaidInvoice
	// Grace is how long past_due keeps its access (EventPeriodEnded).
	Grace time.Duration
}

// Outcome names what a transition did, so a job can handle only its own kind
// (billing.expire past_due/expired, billing.apply_scheduled renewals and
// cancellations) while deterministic rules decide between them.
type Outcome string

const (
	OutcomeTrialStarted Outcome = "trial_started" // free -> trialing
	OutcomeActivated    Outcome = "activated"     // a checkout was paid: new period starting now
	OutcomeRenewed      Outcome = "renewed"       // rolled over into the next, paid period
	OutcomePaidAhead    Outcome = "paid_ahead"    // renewal paid before the period ended: nothing changes yet
	OutcomePastDue      Outcome = "past_due"      // period ended unpaid: grace starts
	OutcomeExpired      Outcome = "expired"       // trial or grace ended unpaid
	OutcomeCanceled     Outcome = "canceled"      // canceled at period end, or organization deleted
)

// Result is the answer of Next.
type Result struct {
	Snapshot Snapshot
	Outcome  Outcome
	// Changed is false when the event was valid but leaves the subscription as
	// it is (OutcomePaidAhead).
	Changed bool
}

// Next applies event to s at time now and returns the resulting snapshot. It
// is the single place that decides what each event does to a subscription: a
// pure function, so it never reads a clock and never fails halfway. An event
// the status does not accept is ErrIllegalTransition, and applying the same
// time-driven event to its own result is refused (ErrIllegalTransition or
// ErrNotDue), which is what makes the jobs that apply it idempotent.
func Next(s Snapshot, event Event, now time.Time) (Result, error) {
	allowed := transitions[s.Status][event.Kind]
	if len(allowed) == 0 {
		return Result{}, fmt.Errorf("%w: %s on a %s subscription", ErrIllegalTransition, event.Kind, s.Status)
	}
	res, err := apply(s, event, now)
	if err != nil {
		return Result{}, err
	}
	if !contains(allowed, res.Snapshot.Status) {
		// A bug in apply, never user input: the table and the rules disagree.
		return Result{}, fmt.Errorf("billing: %s on a %s subscription produced %s, which the transition table does not allow",
			event.Kind, s.Status, res.Snapshot.Status)
	}
	return res, nil
}

func apply(s Snapshot, e Event, now time.Time) (Result, error) {
	switch e.Kind {
	case EventTrialStarted:
		return startTrial(s, e, now)
	case EventInvoicePaid:
		return payInvoice(s, e, now)
	case EventTrialEnded:
		if s.TrialEndsAt == nil || now.Before(*s.TrialEndsAt) {
			return Result{}, ErrNotDue
		}
		s.Status = StatusExpired
		return Result{Snapshot: s, Outcome: OutcomeExpired, Changed: true}, nil
	case EventPeriodEnded:
		return endPeriod(s, e, now)
	case EventGraceEnded:
		if s.GraceUntil == nil || now.Before(*s.GraceUntil) {
			return Result{}, ErrNotDue
		}
		s.Status = StatusExpired
		return Result{Snapshot: s, Outcome: OutcomeExpired, Changed: true}, nil
	case EventOrganizationDeleted:
		s.Status = StatusCanceled
		s.PendingPlanID, s.GraceUntil, s.CancelAtPeriodEnd = nil, nil, false
		return Result{Snapshot: s, Outcome: OutcomeCanceled, Changed: true}, nil
	}
	return Result{}, fmt.Errorf("%w: unknown event %q", ErrInvalidEvent, e.Kind)
}

// startTrial moves a Free subscription onto the trial of a paid plan. Only a
// subscription with no period end (Free) that never trialed qualifies: a trial
// is for organizations that have never paid and never tried.
func startTrial(s Snapshot, e Event, now time.Time) (Result, error) {
	if e.Trial == nil || e.Trial.Days <= 0 {
		return Result{}, fmt.Errorf("%w: a trial needs a plan and a positive number of days", ErrInvalidEvent)
	}
	if s.CurrentPeriodEnd != nil || s.TrialEndsAt != nil {
		return Result{}, ErrNotEligible
	}
	end := now.AddDate(0, 0, e.Trial.Days)
	return Result{Snapshot: Snapshot{
		Status: StatusTrialing, PlanID: e.Trial.PlanID, TrialEndsAt: &end,
		CurrentPeriodStart: now, CurrentPeriodEnd: &end,
	}, Outcome: OutcomeTrialStarted, Changed: true}, nil
}

func payInvoice(s Snapshot, e Event, now time.Time) (Result, error) {
	inv := e.Invoice
	if inv == nil {
		return Result{}, fmt.Errorf("%w: a payment needs its invoice", ErrInvalidEvent)
	}
	switch inv.Kind {
	case InvoiceKindCheckout:
		return activate(s, inv, now)
	case InvoiceKindRenewal:
		return payRenewal(s, inv, now)
	}
	return Result{}, fmt.Errorf("%w: unknown invoice kind %q", ErrInvalidEvent, inv.Kind)
}

// activate handles a paid checkout: from any status the subscription becomes
// active on the invoice's plan, with a new period that starts now (spec §8.4,
// no proration: what was left of the old period is not credited). Pending
// downgrades, cancellation and grace belong to the old arrangement and are
// cleared; the trial marker stays.
func activate(s Snapshot, inv *PaidInvoice, now time.Time) (Result, error) {
	end, err := AddInterval(now, inv.Interval)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrInvalidEvent, err)
	}
	s.Status = StatusActive
	s.PlanID = inv.PlanID
	s.CurrentPeriodStart, s.CurrentPeriodEnd = now, &end
	s.CancelAtPeriodEnd, s.PendingPlanID, s.GraceUntil = false, nil, nil
	return Result{Snapshot: s, Outcome: OutcomeActivated, Changed: true}, nil
}

// payRenewal handles a paid renewal. It only means something to a
// subscription that is renewing, and only for the period right after the
// current one:
//
//   - active: the payment is recorded and nothing changes yet. The new period
//     begins when the current one ends (EventPeriodEnded), so a downgrade
//     scheduled for the period end is not applied early.
//   - past_due: the period already ended, so the renewal takes effect at once
//     and the grace period is over.
//   - anything else is stale (a payment for a subscription that is no longer
//     renewing).
func payRenewal(s Snapshot, inv *PaidInvoice, now time.Time) (Result, error) {
	if s.Status != StatusActive && s.Status != StatusPastDue {
		return Result{}, ErrStaleInvoice
	}
	if s.CurrentPeriodEnd == nil || !inv.PeriodStart.Equal(*s.CurrentPeriodEnd) {
		return Result{}, ErrStaleInvoice
	}
	if s.Status == StatusActive {
		return Result{Snapshot: s, Outcome: OutcomePaidAhead, Changed: false}, nil
	}
	return Result{Snapshot: rollOver(s, inv), Outcome: OutcomeRenewed, Changed: true}, nil
}

// rollOver moves s into the period of a paid renewal on that invoice's plan. A
// scheduled downgrade is consumed when the renewal was billed for that plan.
func rollOver(s Snapshot, inv *PaidInvoice) Snapshot {
	end := inv.PeriodEnd
	s.Status = StatusActive
	s.PlanID = inv.PlanID
	s.CurrentPeriodStart, s.CurrentPeriodEnd = inv.PeriodStart, &end
	s.GraceUntil = nil
	if s.PendingPlanID != nil && *s.PendingPlanID == inv.PlanID {
		s.PendingPlanID = nil
	}
	return s
}

// endPeriod decides what a finished period turns into. The rules, in order:
// a subscription set to cancel is canceled (even if the next period was paid:
// there are no refunds in Tier 1); a paid next period is rolled into; otherwise
// the renewal went unpaid and the subscription is past due, with access kept
// until the grace period (measured from the period end, not from when a job
// noticed) is over.
func endPeriod(s Snapshot, e Event, now time.Time) (Result, error) {
	if s.CurrentPeriodEnd == nil || now.Before(*s.CurrentPeriodEnd) {
		return Result{}, ErrNotDue
	}
	switch {
	case s.CancelAtPeriodEnd:
		s.Status = StatusCanceled
		s.PendingPlanID, s.GraceUntil = nil, nil
		return Result{Snapshot: s, Outcome: OutcomeCanceled, Changed: true}, nil
	case e.NextPeriod != nil:
		if !e.NextPeriod.PeriodStart.Equal(*s.CurrentPeriodEnd) {
			return Result{}, ErrStaleInvoice
		}
		return Result{Snapshot: rollOver(s, e.NextPeriod), Outcome: OutcomeRenewed, Changed: true}, nil
	default:
		s.Status = StatusPastDue
		until := s.CurrentPeriodEnd.Add(e.Grace)
		s.GraceUntil = &until
		return Result{Snapshot: s, Outcome: OutcomePastDue, Changed: true}, nil
	}
}

// ErrNotRenewing: the subscription is not a running paid one (Free, expired or
// canceled), so there is nothing to cancel, resume or downgrade.
var ErrNotRenewing = errors.New("billing: the subscription is not a running paid subscription")

// renewing reports whether s is a paid subscription that is still running: it
// has a period that ends, and has not expired or been canceled.
func renewing(s Snapshot) bool {
	switch s.Status {
	case StatusTrialing, StatusActive, StatusPastDue:
		return s.CurrentPeriodEnd != nil
	}
	return false
}

// RequestCancellation marks a running paid subscription to be canceled when
// its period ends (spec §8.3 cancel). Access continues until then. Asking
// twice changes nothing.
func RequestCancellation(s Snapshot) (Snapshot, error) {
	if !renewing(s) {
		return s, ErrNotRenewing
	}
	s.CancelAtPeriodEnd = true
	return s, nil
}

// RevokeCancellation takes back a pending cancellation (spec §8.3 resume).
// With none pending it changes nothing.
func RevokeCancellation(s Snapshot) (Snapshot, error) {
	if !renewing(s) {
		return s, ErrNotRenewing
	}
	s.CancelAtPeriodEnd = false
	return s, nil
}

// ScheduleDowngrade schedules a move to a cheaper plan at the end of the
// current period (spec §8.4); until then the current plan stays. Only an
// active paid subscription can: a trial or a past due subscription has no
// period to wait for and switches by paying for the new plan instead.
func ScheduleDowngrade(s Snapshot, to uuid.UUID) (Snapshot, error) {
	if s.Status != StatusActive || !renewing(s) {
		return s, ErrNotRenewing
	}
	if to == s.PlanID {
		return s, fmt.Errorf("%w: that is the current plan", ErrInvalidEvent)
	}
	s.PendingPlanID = &to
	return s, nil
}

// CancelDowngrade drops a scheduled downgrade: the current plan continues.
func CancelDowngrade(s Snapshot) Snapshot {
	s.PendingPlanID = nil
	return s
}

// DueEvent returns the time-driven event that is due for s at now, if any:
// the trial that ran out, the period that ended, the grace that ran out. A
// subscription with nothing due (including every Free one, which has no period
// end) returns false. nextPeriod is the paid renewal for the following period,
// if one exists, and grace the length of the grace period.
func DueEvent(s Snapshot, nextPeriod *PaidInvoice, grace time.Duration, now time.Time) (Event, bool) {
	switch s.Status {
	case StatusTrialing:
		if s.TrialEndsAt != nil && !now.Before(*s.TrialEndsAt) {
			return Event{Kind: EventTrialEnded}, true
		}
	case StatusActive:
		if s.CurrentPeriodEnd != nil && !now.Before(*s.CurrentPeriodEnd) {
			return Event{Kind: EventPeriodEnded, NextPeriod: nextPeriod, Grace: grace}, true
		}
	case StatusPastDue:
		if s.GraceUntil != nil && !now.Before(*s.GraceUntil) {
			return Event{Kind: EventGraceEnded}, true
		}
	}
	return Event{}, false
}

// AddInterval returns t advanced by one billing interval ("month" or "year").
// A day that does not exist in the target month (Jan 31 + 1 month) becomes the
// last day of it, instead of spilling into the next month as time.AddDate does.
func AddInterval(t time.Time, interval string) (time.Time, error) {
	var years, months int
	switch interval {
	case "month":
		months = 1
	case "year":
		years = 1
	default:
		return time.Time{}, fmt.Errorf("unknown billing interval %q", interval)
	}
	y, m, d := t.Date()
	target := time.Date(y+years, m+time.Month(months), 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
	last := time.Date(target.Year(), target.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	return time.Date(target.Year(), target.Month(), min(d, last), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location()), nil
}

func contains(list []Status, s Status) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
