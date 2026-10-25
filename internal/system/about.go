package system

import (
	"fmt"
	"runtime"
	"time"
)

// About is the System > Status > About report: build info, run mode and
// process uptime.
type About struct {
	Version       string    `json:"version"`
	Commit        string    `json:"commit"`
	BuildTime     string    `json:"build_time"`
	GoVersion     string    `json:"go_version"`
	Mode          string    `json:"mode"`
	StartedAt     time.Time `json:"started_at"`
	UptimeSeconds int64     `json:"uptime_seconds"`
	Uptime        string    `json:"uptime"`
}

// AboutDeps carries the inputs to BuildAbout. Now is injectable so tests can
// pin the clock; a nil Now uses time.Now.
type AboutDeps struct {
	Version   string
	Commit    string
	BuildTime string
	GoVersion string // empty = runtime.Version()
	Mode      string // empty = "production"
	StartedAt time.Time
	Now       func() time.Time
}

// BuildAbout assembles the about report. Uptime is clamped to non-negative
// (a clock skew or a future StartedAt must never render as negative uptime).
func BuildAbout(d AboutDeps) About {
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	goVersion := d.GoVersion
	if goVersion == "" {
		goVersion = runtime.Version()
	}
	mode := d.Mode
	if mode == "" {
		mode = "production"
	}
	up := now().Sub(d.StartedAt)
	if up < 0 {
		up = 0
	}
	return About{
		Version:       d.Version,
		Commit:        d.Commit,
		BuildTime:     d.BuildTime,
		GoVersion:     goVersion,
		Mode:          mode,
		StartedAt:     d.StartedAt,
		UptimeSeconds: int64(up.Seconds()),
		Uptime:        humanUptime(up),
	}
}

// humanUptime renders a duration as e.g. "3h 12m", "5m 9s" or "42s".
func humanUptime(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm %ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}
