package httpapi

import (
	"sync"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/clock"
)

// rateLimiter is a fixed-window per-user limiter for the play endpoint.
// Injected clock keeps it testable. Sufficient at this scale; swap for a
// shared limiter when API nodes scale out.
type rateLimiter struct {
	mu     sync.Mutex
	window time.Duration
	max    int
	hits   map[int64]*rateBucket
	clk    clock.Clock
}

type rateBucket struct {
	start time.Time
	count int
}

func newRateLimiter(clk clock.Clock, window time.Duration, max int) *rateLimiter {
	return &rateLimiter{window: window, max: max, hits: make(map[int64]*rateBucket), clk: clk}
}

func (l *rateLimiter) allow(userID int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clk.Now()
	b, ok := l.hits[userID]
	if !ok || now.Sub(b.start) >= l.window {
		b = &rateBucket{start: now}
		l.hits[userID] = b
	}
	b.count++
	return b.count <= l.max
}
