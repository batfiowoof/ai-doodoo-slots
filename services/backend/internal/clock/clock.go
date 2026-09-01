// Package clock is the only place in the codebase allowed to call time.Now.
// Everything else receives a Clock so time is injectable and tests are
// deterministic. See guard_test.go, which fails the build if any other
// package calls time.Now directly.
package clock

import (
	"sync"
	"time"
)

// Clock provides the current time.
type Clock interface {
	Now() time.Time
}

// Real is the production Clock, backed by time.Now.
type Real struct{}

// Now returns the wall-clock time.
func (Real) Now() time.Time { return time.Now() }

// Fake is a manually-advanced Clock for tests. Safe for concurrent use so
// round loops and timers can share it.
type Fake struct {
	mu      sync.Mutex
	current time.Time
}

// NewFake returns a Fake Clock starting at t.
func NewFake(t time.Time) *Fake {
	return &Fake{current: t}
}

// Now returns the fake's current time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.current
}

// Advance moves the fake clock forward by d.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.current = f.current.Add(d)
}

// Set jumps the fake clock to t.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.current = t
}
