package billing_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kangsolder001/saka/internal/billing"
)

func TestGrantsAccess_FollowsTheStatusTable(t *testing.T) {
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	grace := func(d time.Duration) *time.Time { t := now.Add(d); return &t }

	tests := map[string]struct {
		status billing.Status
		grace  *time.Time
		want   bool
	}{
		"trialing":                       {billing.StatusTrialing, nil, true},
		"active":                         {billing.StatusActive, nil, true},
		"active ignores a stale grace":   {billing.StatusActive, grace(-time.Hour), true},
		"past_due inside the grace":      {billing.StatusPastDue, grace(time.Second), true},
		"past_due on the last instant":   {billing.StatusPastDue, grace(0), false},
		"past_due after the grace":       {billing.StatusPastDue, grace(-time.Second), false},
		"past_due without a grace end":   {billing.StatusPastDue, nil, false},
		"expired":                        {billing.StatusExpired, nil, false},
		"expired even with a future end": {billing.StatusExpired, grace(time.Hour), false},
		"canceled":                       {billing.StatusCanceled, nil, false},
		"unknown status fails closed":    {billing.Status("paused"), nil, false},
		"empty status fails closed":      {billing.Status(""), nil, false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, billing.GrantsAccess(tt.status, tt.grace, now))
		})
	}
}
