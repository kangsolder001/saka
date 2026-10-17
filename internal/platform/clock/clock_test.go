package clock_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kangsolder001/saka/internal/platform/clock"
)

func TestReal_NowIsUTC(t *testing.T) {
	now := clock.Real{}.Now()

	assert.Equal(t, time.UTC, now.Location())
	assert.WithinDuration(t, time.Now(), now, time.Second)
}

func TestFake_StartsAtGivenTimeInUTC(t *testing.T) {
	jakarta := time.FixedZone("WIB", 7*3600)
	start := time.Date(2026, 1, 1, 7, 0, 0, 0, jakarta)

	f := clock.NewFake(start)

	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), f.Now())
	assert.Equal(t, time.UTC, f.Now().Location())
}

func TestFake_AdvanceAndSet(t *testing.T) {
	f := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	f.Advance(90 * time.Minute)
	assert.Equal(t, time.Date(2026, 1, 1, 1, 30, 0, 0, time.UTC), f.Now())

	f.Set(time.Date(2027, 6, 1, 12, 0, 0, 0, time.UTC))
	assert.Equal(t, time.Date(2027, 6, 1, 12, 0, 0, 0, time.UTC), f.Now())
}

func TestFake_ConcurrentUse(t *testing.T) {
	f := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(2)
		go func() { defer wg.Done(); f.Advance(time.Second) }()
		go func() { defer wg.Done(); _ = f.Now() }()
	}
	wg.Wait()

	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 50, 0, time.UTC), f.Now())
}
