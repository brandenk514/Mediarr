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

// refillWindow is how often the token bucket refills. The fork's
// axios-rate-limit default window is 100ms: tokens are granted in
// window-sized batches (rps × window per tick), never as a continuous
// drip — a burst is absorbed instantly, and a drained bucket refills
// on the next window boundary.
const refillWindow = 100 * time.Millisecond

// hostLimiter bounds the traffic to one upstream host: a token pool
// (the slots channel, capacity == concurrent — the burst) refilled at
// rps tokens per second in refillWindow batches. Acquire blocks until a
// token is free; Release (deferred by the caller) returns it to the
// pool, so at most `concurrent` requests admitted without a release can
// be in flight against the host.
type hostLimiter struct {
	slots chan struct{}
	burst float64
	rps   float64

	mu     sync.Mutex
	tokens float64
	last   time.Time
}

func newHostLimiter(concurrent int, rps float64) *hostLimiter {
	l := &hostLimiter{
		slots: make(chan struct{}, concurrent),
		burst: float64(concurrent),
		rps:   rps,
		last:  time.Now(),
	}
	for i := 0; i < concurrent; i++ {
		l.slots <- struct{}{}
	}
	return l
}

// acquire takes one token and one concurrency slot, blocking until both
// are available (the context cancels the wait).
func (l *hostLimiter) acquire(ctx context.Context) error {
	for {
		// Fast path: a token (or a release) is already available.
		select {
		case <-l.slots:
			return nil
		default:
		}
		l.refill()
		select {
		case <-l.slots:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(refillWindow):
			// next iteration: batch-refill and retry
		}
	}
}

// release returns the concurrency slot.
func (l *hostLimiter) release() {
	// The pool is capped at burst: extra returns (e.g. a release after
	// a refill) are dropped rather than blocking.
	select {
	case l.slots <- struct{}{}:
	default:
	}
}

// refill grants the tokens accumulated since the last window (credit for
// a fractional token is carried over), pushing them into the pool.
func (l *hostLimiter) refill() {
	l.mu.Lock()
	now := time.Now()
	owed := l.tokens + now.Sub(l.last).Seconds()*l.rps
	l.last = now
	if owed < 1 {
		l.tokens = owed
		l.mu.Unlock()
		return
	}
	l.tokens = 0
	l.mu.Unlock()
	for i := 0; i < int(owed); i++ {
		select {
		case l.slots <- struct{}{}:
		default:
			return // pool full: the excess is dropped (burst cap)
		}
	}
}
