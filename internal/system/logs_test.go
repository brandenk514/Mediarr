package system

import (
	"testing"
	"time"
)

func TestLogRingBufferBounded(t *testing.T) {
	l := NewLogService(3)
	for _, msg := range []string{"a", "b", "c", "d", "e"} {
		l.Append(LogInfo, msg)
	}
	rec := l.Recent()
	if len(rec) != 3 {
		t.Fatalf("expected 3 buffered lines, got %d", len(rec))
	}
	// Oldest-to-newest; the buffer keeps the most recent three (c, d, e).
	if rec[0].Message != "c" || rec[1].Message != "d" || rec[2].Message != "e" {
		t.Errorf("unexpected backlog order: %v", rec)
	}
}

func TestLogSubscribeReceivesLiveLines(t *testing.T) {
	l := NewLogService(10)
	ch, cancel := l.Subscribe()
	defer cancel()

	l.Append(LogWarn, "disk low")
	l.Append(LogError, "db down")

	select {
	case line := <-ch:
		if line.Message != "disk low" || line.Level != LogWarn {
			t.Errorf("unexpected first line: %+v", line)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for a live line")
	}
	select {
	case line := <-ch:
		if line.Message != "db down" || line.Level != LogError {
			t.Errorf("unexpected second line: %+v", line)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the second live line")
	}
}

func TestLogSubscribeCancelStopsDelivery(t *testing.T) {
	l := NewLogService(10)
	ch, cancel := l.Subscribe()
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel closed after cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed after cancel")
	}
}

func TestLogAppendDoesNotStallOnSlowSubscriber(t *testing.T) {
	l := NewLogService(10)
	_, cancel := l.Subscribe()
	defer cancel()
	// Do NOT read from the subscriber; its 64-slot buffer fills, then Appends
	// drop for the slow subscriber instead of blocking.
	for range 200 {
		l.Append(LogInfo, "line")
	}
	// The backlog still grows to the ring cap.
	if got := len(l.Recent()); got != 10 {
		t.Errorf("expected 10 buffered lines, got %d", got)
	}
}

func TestLogServiceMinCapacity(t *testing.T) {
	l := NewLogService(0)
	l.Append(LogInfo, "x")
	if got := len(l.Recent()); got != 1 {
		t.Errorf("expected 1 buffered line with clamped capacity, got %d", got)
	}
}
