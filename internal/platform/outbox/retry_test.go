package outbox_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kangsolder001/saka/internal/platform/outbox"
)

func TestRetryDelay_FollowsSpecSchedule(t *testing.T) {
	want := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}

	for i, d := range want {
		got, ok := outbox.RetryDelay(i + 1)

		assert.True(t, ok, "attempt %d must be retried", i+1)
		assert.Equal(t, d, got, "attempt %d", i+1)
	}
}

func TestRetryDelay_GivesUpAfterLastRetry(t *testing.T) {
	_, ok := outbox.RetryDelay(outbox.MaxAttempts)

	assert.False(t, ok, "failure of the final attempt is permanent")
	assert.Equal(t, 6, outbox.MaxAttempts, "initial attempt + 5 retries")
}

func TestRetryDelay_RejectsInvalidAttemptCounts(t *testing.T) {
	for _, attempts := range []int{0, -1, 100} {
		_, ok := outbox.RetryDelay(attempts)

		assert.False(t, ok, "attempts=%d", attempts)
	}
}
