package indexers

import (
	"context"
	"sync"
	"time"
)

// RateLimiter is a small, dependency-free token bucket used to bound how
// often an indexer may be hit (PLAN §5: "rate limiting"). It is deliberately
// in-process: the M5 requirement is to *shape* per-indexer polling so a busy
// feed (TorrentRSS) or a strict Torznab does not get hammered. A cross-process
// (Redis) bucket is an extension, not a requirement — each mediarr process
// owns its own limiter, and a single process is the unit that hammers an
// indexer.
//
// A limiter is created once per indexer (keyed by name) and shared by that
// indexer's adapter and any periodic pollers.
type RateLimiter struct {
	mu       sync.Mutex
	capacity float64 // max tokens (burst)
	tokens   float64 // current tokens
	refill   float64 // tokens added per second
	last     time.Time
}

// NewRateLimiter builds a limiter allowing `rate` requests per second with a
// burst of up to `burst` requests. rate <= 0 disables limiting (Wait always
// returns immediately); burst < 1 is clamped to 1.
func NewRateLimiter(rate float64, burst int) *RateLimiter {
	if burst < 1 {
		burst = 1
	}
	return &RateLimiter{
		capacity: float64(burst),
		tokens:   float64(burst),
		refill:   rate,
		last:     time.Now(),
	}
}

// Wait blocks until a token is available (or ctx is done) and consumes it.
// It returns the context error if the context is cancelled before a token
// frees up. A disabled limiter (rate <= 0) never blocks.
func (rl *RateLimiter) Wait(ctx context.Context) error {
	for {
		delay := rl.take()
		if delay <= 0 {
			return nil // a token was available; already consumed
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

// take atomically tries to consume a token. It returns 0 if a token was taken,
// or the duration until the next token is available otherwise.
func (rl *RateLimiter) take() time.Duration {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if rl.refill <= 0 {
		return 0 // unlimited
	}
	now := time.Now()
	// Refill based on elapsed time, capped at capacity.
	elapsed := now.Sub(rl.last).Seconds()
	rl.last = now
	rl.tokens = min(rl.capacity, rl.tokens+elapsed*rl.refill)
	if rl.tokens >= 1 {
		rl.tokens--
		return 0
	}
	// Time until we have a full token.
	missing := 1 - rl.tokens
	return time.Duration(missing / rl.refill * float64(time.Second))
}

// DefaultPollRate is the default requests-per-second ceiling for a single
// indexer adapter. Torznab/TorrentRSS indexes are typically fine with a few
// requests per second; this keeps a 10-minute search cache (PLAN §5) and a
// periodic poller well inside most indexes' courtesy limits.
const DefaultPollRate = 5.0

// DefaultBurst allows a short initial burst (e.g. a fan-out over several
// enabled indexers, or a first poll) before the rate ceiling applies.
const DefaultBurst = 5
