package outbox

import "time"

// retrySchedule is the wait before each retry (spec §10.1): 1m, 5m, 30m, 2h, 6h.
var retrySchedule = [...]time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
	6 * time.Hour,
}

// MaxAttempts is the total number of executions a job gets: the initial
// attempt plus one per retry in the schedule.
const MaxAttempts = len(retrySchedule) + 1

// RetryDelay returns how long to wait before the next attempt, given how many
// attempts have already been made (attempts >= 1). ok is false once the job
// has used up its retries and must be marked failed.
func RetryDelay(attempts int) (delay time.Duration, ok bool) {
	if attempts < 1 || attempts > len(retrySchedule) {
		return 0, false
	}
	return retrySchedule[attempts-1], true
}
