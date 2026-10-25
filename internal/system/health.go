// Package system is the service layer for the System area (P6): the
// structured health table, the task (worker-inventory) registry, disk usage
// for the configured media roots, the about report, backup export/import,
// the changelog, the streaming log buffer, and the Sonarr drop-in migration.
//
// Like internal/queue and internal/settings, it is a pure domain layer with no
// I/O of its own beyond the explicitly injectable probes; the API layer
// (internal/api/system.go) assembles the probes from the running system and
// the Postgres/Redis adapters, and main.go wires it. Keeping the logic here
// (with injectable pingers, clocks and filesystem stat) is what makes it
// unit-testable without a live Postgres/Redis/Docker.
package system

import (
	"context"
	"fmt"
)

// Severity is the outcome level of a single health check.
type Severity string

// The three severities, mirroring Sonarr's health widget.
const (
	SeverityOK      Severity = "ok"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// HealthCheck is one row in the System > Status health table: a check id, a
// human message, a severity, and a wiki/docs link.
type HealthCheck struct {
	Check    string   `json:"check"`
	Message  string   `json:"message"`
	Severity Severity `json:"severity"`
	WikiLink string   `json:"wiki_link"`
}

// SystemHealth is the full health report. The top-level counts are what the
// System nav badge reads (the number of non-ok checks).
type SystemHealth struct {
	Checks  []HealthCheck `json:"checks"`
	OK      int           `json:"ok"`
	Warning int           `json:"warning"`
	Error   int           `json:"error"`
}

// NonOK returns warning + error — the value the System nav badge shows.
func (r SystemHealth) NonOK() int { return r.Warning + r.Error }

// Probe is one dependency check. Exactly one of Ping or Static should be set:
// Ping pings a live dependency (an error means down), Static reports a fixed
// configured/not-configured condition. Soft probes degrade a failure to a
// warning (e.g. an optional provider key) rather than an error.
type Probe struct {
	Name     string
	WikiLink string
	// Ping returns an error when the dependency is down (nil = up).
	Ping func(ctx context.Context) error
	// Static returns (ok, message) for a condition-based check.
	Static func() (bool, string)
	// Soft: a failure is a warning, not an error.
	Soft bool
}

// Health aggregates the probes and produces the structured report.
type Health struct {
	probes []Probe
}

// NewHealth builds a Health over the given probes.
func NewHealth(probes []Probe) *Health { return &Health{probes: probes} }

// Report runs every probe and returns the structured health report. Checks
// are always returned in probe order and never nil (an empty report is an
// empty [] rather than null).
func (h *Health) Report(ctx context.Context) SystemHealth {
	rep := SystemHealth{Checks: []HealthCheck{}}
	for _, p := range h.probes {
		chk := HealthCheck{Check: p.Name, WikiLink: p.WikiLink}
		ok, msg := false, ""
		switch {
		case p.Static != nil:
			ok, msg = p.Static()
		case p.Ping != nil:
			err := p.Ping(ctx)
			ok = err == nil
			if err != nil {
				msg = err.Error()
			}
		}
		if ok {
			chk.Severity = SeverityOK
			if msg == "" {
				msg = "ok"
			}
		} else if p.Soft {
			chk.Severity = SeverityWarning
			if msg == "" {
				msg = "not configured"
			}
		} else {
			chk.Severity = SeverityError
			if msg == "" {
				msg = "unavailable"
			}
		}
		chk.Message = msg
		rep.Checks = append(rep.Checks, chk)
		switch chk.Severity {
		case SeverityOK:
			rep.OK++
		case SeverityWarning:
			rep.Warning++
		case SeverityError:
			rep.Error++
		}
	}
	return rep
}

// errf is a small helper for building probe error messages.
func errf(format string, args ...any) error { return fmt.Errorf(format, args...) }
