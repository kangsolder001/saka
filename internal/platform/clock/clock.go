// Package clock abstracts time so business rules can be tested with a
// controllable clock. Application code must not call time.Now directly.
package clock

import (
	"sync"
	"time"
)

// Clock returns the current time, always in UTC.
type Clock interface {
	Now() time.Time
}

// Real is the production Clock backed by the system time.
type Real struct{}

// Now returns the current system time in UTC.
func (Real) Now() time.Time { return time.Now().UTC() }

// Fake is a Clock whose time only moves when told to. It is safe for
// concurrent use.
type Fake struct {
	mu  sync.RWMutex
	now time.Time
}

// NewFake creates a Fake starting at the given instant (converted to UTC).
func NewFake(start time.Time) *Fake {
	return &Fake{now: start.UTC()}
}

// Now returns the fake's current time.
func (f *Fake) Now() time.Time {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.now
}

// Set moves the fake to an absolute instant.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t.UTC()
}

// Advance moves the fake forward (or backward, for a negative duration).
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}
