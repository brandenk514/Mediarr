package movies

import (
	"testing"
	"time"
)

func TestDeriveMovieStatus(t *testing.T) {
	now := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	past := now.Add(-24 * time.Hour)
	future := now.Add(24 * time.Hour)

	cases := []struct {
		name       string
		downloaded bool
		activeQ    bool
		monitored  bool
		release    *time.Time
		want       MovieStatus
	}{
		{"downloaded monitored", true, false, true, nil, StatusDownloadedMonitored},
		{"downloaded unmonitored", true, false, false, nil, StatusDownloadedUnmonitored},
		{"downloaded wins over queue", true, true, true, nil, StatusDownloadedMonitored},
		{"downloaded beats unreleased date", true, false, true, &future, StatusDownloadedMonitored},
		{"queued monitored", false, true, true, nil, StatusQueued},
		{"queued unmonitored", false, true, false, nil, StatusQueued},
		{"queued wins over future release", false, true, true, &future, StatusQueued},
		{"missing monitored", false, false, true, nil, StatusMissingMonitored},
		{"missing unmonitored", false, false, false, nil, StatusMissingUnmonitored},
		{"unreleased beats missing", false, false, true, &future, StatusUnreleased},
		{"released movie is not unreleased", false, false, true, &past, StatusMissingMonitored},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DeriveMovieStatus(tc.downloaded, tc.activeQ, tc.monitored, tc.release, now)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDeriveMovieStatus_NonUTCNow: with now in a zone behind UTC whose local
// date lags the UTC date, "today" is the UTC date: a release on the UTC date
// is unreleased; a release on the local date (= UTC yesterday) is missing,
// split by the monitored flag (issue #139).
func TestDeriveMovieStatus_NonUTCNow(t *testing.T) {
	// 2026-11-25 21:00 EST = 2026-11-26 02:00 UTC.
	now := time.Date(2026, 11, 25, 21, 0, 0, 0, time.FixedZone("EST", -5*3600))
	utcToday := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	utcYesterday := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		monitored bool
		release   *time.Time
		want      MovieStatus
	}{
		{"release UTC today is unreleased (monitored)", true, &utcToday, StatusUnreleased},
		{"release UTC today is unreleased (unmonitored)", false, &utcToday, StatusUnreleased},
		{"release UTC yesterday is missing (monitored)", true, &utcYesterday, StatusMissingMonitored},
		{"release UTC yesterday is missing (unmonitored)", false, &utcYesterday, StatusMissingUnmonitored},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DeriveMovieStatus(false, false, tc.monitored, tc.release, now)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
