package imageproxy

import (
	"context"
	"sync"
	"time"
)

// Rate-limit shape (the fork's axios-rate-limit options): 20 max requests,
// 50 max requests-per-second, per upstream host.
const (
	maxConcurrentPerHost = 20
	maxRPSPerHost        = 50.0
)

// hostLimiter bounds the traffic to one upstream host: at most
// `concurrent` in-flight requests, admitted through a token bucket with
// burst == concurrent refilling at rps tokens/second. Acquire blocks
// until both a concurrency slot and a token are available; Release
// (deferred by the caller) frees the slot.
type hostLimiter struct {
	slots chan struct{}
	burst float64
	rps   float64

	mu     sync.Mutex
	tokens float64
	last   time.Time
}

func newHostLimiter(concurrent int, rps float64) *hostLimiter {
	return nil
}

// acquire takes one token and one concurrency slot, blocking until both
// are available (the context cancels the wait).
func (l *hostLimiter) acquire(ctx context.Context) error {
	return ErrNotImplemented
}

// release returns the concurrency slot.
func (l *hostLimiter) release() {}
