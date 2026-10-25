package system

import (
	"testing"
	"time"
)

func TestBuildAboutUptimeAndDefaults(t *testing.T) {
	start := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	now := time.Date(2026, 10, 25, 15, 12, 5, 0, time.UTC) // 3h12m5s later
	a := BuildAbout(AboutDeps{
		Version:   "v0.1.0",
		Commit:    "abc123",
		BuildTime: "2026-10-25T00:00:00Z",
		Mode:      "development",
		StartedAt: start,
		Now:       func() time.Time { return now },
	})
	if a.Version != "v0.1.0" || a.Commit != "abc123" || a.BuildTime != "2026-10-25T00:00:00Z" {
		t.Fatalf("build echo wrong: %+v", a)
	}
	if a.Mode != "development" {
		t.Fatalf("mode=%q", a.Mode)
	}
	if a.UptimeSeconds != int64((3*time.Hour + 12*time.Minute + 5*time.Second).Seconds()) {
		t.Fatalf("uptime_seconds=%d", a.UptimeSeconds)
	}
	if a.Uptime != "3h 12m" {
		t.Fatalf("uptime=%q want 3h 12m", a.Uptime)
	}
	if a.GoVersion == "" {
		t.Fatalf("go version should default to runtime")
	}
}

func TestBuildAboutDefaultsForEmpty(t *testing.T) {
	a := BuildAbout(AboutDeps{
		StartedAt: time.Date(2026, 1, 1, 0, 0, 10, 0, time.UTC),
		Now:       func() time.Time { return time.Date(2026, 1, 1, 0, 0, 10, 0, time.UTC) },
	})
	if a.Mode != "production" {
		t.Fatalf("default mode=%q want production", a.Mode)
	}
	if a.Uptime != "0s" {
		t.Fatalf("uptime=%q want 0s", a.Uptime)
	}
}

func TestBuildAboutNegativeUptimeClamped(t *testing.T) {
	a := BuildAbout(AboutDeps{
		StartedAt: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), // future
		Now:       func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	if a.UptimeSeconds < 0 {
		t.Fatalf("uptime_seconds=%d should be clamped to >=0", a.UptimeSeconds)
	}
	if a.Uptime != "0s" {
		t.Fatalf("uptime=%q want 0s", a.Uptime)
	}
}

func TestHumanUptimeFormats(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{time.Second * 42, "42s"},
		{time.Minute*5 + 9*time.Second, "5m 9s"},
		{time.Hour*3 + 12*time.Minute, "3h 12m"},
		{0, "0s"},
	}
	for _, c := range cases {
		if got := humanUptime(c.d); got != c.want {
			t.Errorf("humanUptime(%s)=%q want %q", c.d, got, c.want)
		}
	}
}
