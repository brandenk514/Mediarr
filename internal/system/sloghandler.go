package system

import (
	"context"
	"log/slog"
)

// slogBridge is a slog.Handler that forwards every record to a LogWriter (the
// streaming log viewer source) in addition to delegating to an inner handler
// (so logs still go to stdout/stderr). This lets the Log Files page show the
// real application logs without the logger importing the system package.
type slogBridge struct {
	inner slog.Handler
	w     LogWriter
}

// NewSlogBridge wraps inner so each logged record is also appended to w.
func NewSlogBridge(inner slog.Handler, w LogWriter) slog.Handler {
	return &slogBridge{inner: inner, w: w}
}

func (b *slogBridge) Enabled(ctx context.Context, level slog.Level) bool {
	return b.inner.Enabled(ctx, level)
}

func (b *slogBridge) Handle(ctx context.Context, r slog.Record) error {
	if b.w != nil {
		b.w.Append(slogLevelToLogLevel(r.Level), r.Message)
	}
	return b.inner.Handle(ctx, r)
}

func (b *slogBridge) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &slogBridge{inner: b.inner.WithAttrs(attrs), w: b.w}
}

func (b *slogBridge) WithGroup(name string) slog.Handler {
	return &slogBridge{inner: b.inner.WithGroup(name), w: b.w}
}

func slogLevelToLogLevel(l slog.Level) LogLevel {
	switch {
	case l <= slog.LevelDebug:
		return LogDebug
	case l <= slog.LevelInfo:
		return LogInfo
	case l <= slog.LevelWarn:
		return LogWarn
	default:
		return LogError
	}
}
