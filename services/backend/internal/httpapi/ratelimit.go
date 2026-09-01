package httpapi

import (
	"strconv"
	"sync"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/clock"
)

// rateLimiter is a fixed-window limiter for the play endpoint (user IDs)
// and auth endpoints (IPs). Injected clock keeps it testable. Sufficient at
// this scale; swap for a shared limiter when API nodes scale out.
type rateLimiter struct {
	mu     sync.Mutex
	window time.Duration
	max    int
	hits   map[string]*rateBucket
	clk    clock.Clock
}

type rateBucket struct {
	start time.Time
	count int
}

func newRateLimiter(clk clock.Clock, window time.Duration, max int) *rateLimiter {
	return &rateLimiter{window: window, max: max, hits: make(map[string]*rateBucket), clk: clk}
}

func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clk.Now()
	b, ok := l.hits[key]
	if !ok || now.Sub(b.start) >= l.window {
		b = &rateBucket{start: now}
		l.hits[key] = b
	}
	b.count++
	return b.count <= l.max
}

// allowUserID is a convenience for per-user limiting.
func (l *rateLimiter) allowUserID(userID int64) bool {
	return l.allow(strconv.FormatInt(userID, 10))
}
