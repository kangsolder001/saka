package billing

import "time"

// GrantsAccess reports whether a subscription in this state gives access to
// its plan (spec §8.2): trialing and active do, past_due only until the grace
// period ends, expired and canceled never. Everything else, including an
// unknown status, does not (fail closed). It is a pure function so the
// transitions built on it can be tested with a fake clock.
func GrantsAccess(status Status, graceUntil *time.Time, now time.Time) bool {
	switch status {
	case StatusTrialing, StatusActive:
		return true
	case StatusPastDue:
		return graceUntil != nil && now.Before(*graceUntil)
	default:
		return false
	}
}
