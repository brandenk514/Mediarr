package system

import (
	"sync"
	"time"
)

// ---- P6 System > Log Files: streaming log viewer over SSE ----
//
// The log service is an in-process ring buffer of recent application log
// lines plus a subscriber fan-out. The API layer serves it over
// Server-Sent-Events: the first frames are the ring-buffer backlog, then live
// lines are pushed as they occur (flushed per event). The app's slog handler
// is wired to Append so the viewer reflects the real application logs.

// LogLevel is a normalized log level tag (info / warn / error / debug).
type LogLevel string

const (
	LogDebug LogLevel = "debug"
	LogInfo  LogLevel = "info"
	LogWarn  LogLevel = "warn"
	LogError LogLevel = "error"
)

// LogLine is one application log line in the viewer.
type LogLine struct {
	Time    time.Time `json:"time"`
	Level   LogLevel  `json:"level"`
	Message string    `json:"message"`
}

// LogService is a thread-safe ring buffer of recent log lines with a live
// subscriber fan-out for the SSE stream.
type LogService struct {
	mu    sync.RWMutex
	buf   []LogLine
	cap   int
	head  int // next insert index (ring)
	count int // number of live lines (<= cap)
	subs  map[chan LogLine]struct{}
	nowFn func() time.Time
}

// NewLogService builds a log service with the given ring-buffer capacity
// (clamped to a minimum of 1).
func NewLogService(capacity int) *LogService {
	if capacity < 1 {
		capacity = 1
	}
	return &LogService{
		buf:  make([]LogLine, 0, capacity),
		cap:  capacity,
		subs: map[chan LogLine]struct{}{},
		nowFn: func() time.Time {
			return time.Now()
		},
	}
}

// Append records a log line: it is added to the ring buffer and fanned out to
// every live subscriber (non-blocking, so a slow reader never stalls the app).
func (l *LogService) Append(level LogLevel, message string) {
	line := LogLine{Time: l.nowFn(), Level: level, Message: message}

	l.mu.Lock()
	l.buf = append(l.buf, line)
	if len(l.buf) > l.cap {
		l.buf = l.buf[len(l.buf)-l.cap:]
	}
	for ch := range l.subs {
		select {
		case ch <- line:
		default:
			// Drop for a slow subscriber; the backlog still carries it.
		}
	}
	l.mu.Unlock()
}

// Recent returns the ring-buffer backlog, oldest to newest.
func (l *LogService) Recent() []LogLine {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]LogLine, len(l.buf))
	copy(out, l.buf)
	return out
}

// Subscribe registers a live subscriber and returns its channel plus a cancel
// func. The channel is buffered (64) to absorb bursts without blocking.
func (l *LogService) Subscribe() (<-chan LogLine, func()) {
	ch := make(chan LogLine, 64)
	l.mu.Lock()
	l.subs[ch] = struct{}{}
	l.mu.Unlock()
	cancel := func() {
		l.mu.Lock()
		if _, ok := l.subs[ch]; ok {
			delete(l.subs, ch)
			close(ch)
		}
		l.mu.Unlock()
	}
	return ch, cancel
}

// LogWriter adapts a LogService to a minimal writer seam so the app's logger
// can be wired to it without importing the system package into the logger.
// It only exposes the two calls a slog adapter needs.
type LogWriter interface {
	Append(level LogLevel, message string)
}
