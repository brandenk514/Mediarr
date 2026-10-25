package system

import (
	"context"
	"errors"
	"testing"
)

func TestHealthReportCountsAndOrder(t *testing.T) {
	var ctx context.Context
	h := NewHealth([]Probe{
		{Name: "postgres", Ping: func(context.Context) error { return nil }},
		{Name: "redis", Ping: func(context.Context) error { return errors.New("dial refused") }},
		{Name: "plex", Soft: true, Static: func() (bool, string) { return false, "no plex key" }},
		{Name: "media_root", Static: func() (bool, string) { return true, "/data/media exists" }},
	})
	rep := h.Report(ctx)

	if len(rep.Checks) != 4 {
		t.Fatalf("expected 4 checks, got %d", len(rep.Checks))
	}
	if rep.OK != 2 || rep.Warning != 1 || rep.Error != 1 {
		t.Fatalf("counts wrong: ok=%d warn=%d err=%d (want 2/1/1)", rep.OK, rep.Warning, rep.Error)
	}
	if rep.NonOK() != 2 {
		t.Fatalf("NonOK()=%d want 2", rep.NonOK())
	}
	// Order preserved and severities correct.
	want := []Severity{SeverityOK, SeverityError, SeverityWarning, SeverityOK}
	for i, c := range rep.Checks {
		if c.Severity != want[i] {
			t.Errorf("check %d severity=%s want %s", i, c.Severity, want[i])
		}
	}
	if rep.Checks[1].Message != "dial refused" {
		t.Errorf("redis message=%q", rep.Checks[1].Message)
	}
	// ok with empty message defaults to "ok".
	if rep.Checks[0].Message != "ok" {
		t.Errorf("postgres message=%q want ok", rep.Checks[0].Message)
	}
	if rep.Checks[2].Message != "no plex key" {
		t.Errorf("plex message=%q", rep.Checks[2].Message)
	}
}

func TestHealthReportEmpty(t *testing.T) {
	rep := NewHealth(nil).Report(context.Background())
	if len(rep.Checks) != 0 || rep.OK != 0 || rep.NonOK() != 0 {
		t.Fatalf("empty report not zeroed: %+v", rep)
	}
	if rep.Checks == nil {
		t.Fatalf("checks should be non-nil empty slice")
	}
}

func TestHealthPingErrorDefaultMessage(t *testing.T) {
	h := NewHealth([]Probe{{Name: "db", Ping: func(context.Context) error { return errors.New("") }}})
	rep := h.Report(context.Background())
	if rep.Checks[0].Severity != SeverityError {
		t.Fatalf("severity=%s want error", rep.Checks[0].Severity)
	}
	// empty error string -> default "unavailable".
	if rep.Checks[0].Message != "unavailable" {
		t.Fatalf("message=%q want unavailable", rep.Checks[0].Message)
	}
}

func TestHealthSoftFailureIsWarning(t *testing.T) {
	h := NewHealth([]Probe{{Name: "opt", Soft: true, Ping: func(context.Context) error { return errors.New("x") }}})
	rep := h.Report(context.Background())
	if rep.Checks[0].Severity != SeverityWarning {
		t.Fatalf("severity=%s want warning", rep.Checks[0].Severity)
	}
}
