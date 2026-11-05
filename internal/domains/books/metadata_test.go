package books

import (
	"testing"
	"time"
)

// TestIsFresh is the MT3 TTL decision (pure logic): a row fetched at
// fetchedAt is fresh until fetchedAt+ttl, exactly at the boundary it is
// still fresh, and past the boundary it is stale.
func TestIsFresh(t *testing.T) {
	fetched := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	ttl := 7 * 24 * time.Hour

	cases := []struct {
		name  string
		now   time.Time
		ttl   time.Duration
		fresh bool
	}{
		{"just fetched", fetched, ttl, true},
		{"within ttl", fetched.Add(ttl - time.Hour), ttl, true},
		{"exactly at boundary is fresh", fetched.Add(ttl), ttl, true},
		{"one second past ttl", fetched.Add(ttl + time.Second), ttl, false},
		{"long past ttl", fetched.Add(30 * 24 * time.Hour), ttl, false},
		{"short ttl respected", fetched.Add(10 * time.Minute), time.Hour, true},
		{"short ttl expired", fetched.Add(2 * time.Hour), time.Hour, false},
		{"clock skew (fetched in future) stays fresh", fetched.Add(time.Hour), ttl, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsFresh(fetched, tc.now, tc.ttl); got != tc.fresh {
				t.Errorf("IsFresh = %v, want %v", got, tc.fresh)
			}
		})
	}
}
