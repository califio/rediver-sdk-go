package rediver

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// jobSlogAdapter is a slog.Handler that converts each slog.Record into a
// rediver.Event by emitting a Log event through the Job's emit channel.
// Used by Job.SlogHandler() so third-party libs taking *slog.Logger can be
// wired into the event stream. KV pairs are concatenated into the message
// string (lossy by design — strict NewLog has no fields).
type jobSlogAdapter struct {
	emit  func(Event)
	level slog.Level
	attrs []slog.Attr
}

func newJobSlogAdapter(emit func(Event), level slog.Level) *jobSlogAdapter {
	return &jobSlogAdapter{emit: emit, level: level}
}

func (a *jobSlogAdapter) Enabled(_ context.Context, l slog.Level) bool {
	return l >= a.level
}

func (a *jobSlogAdapter) Handle(_ context.Context, r slog.Record) error {
	level := slogLevelToLogLevel(r.Level)
	message := r.Message
	if len(a.attrs) > 0 || r.NumAttrs() > 0 {
		var b strings.Builder
		b.WriteString(message)
		for _, attr := range a.attrs {
			fmt.Fprintf(&b, " %s=%v", attr.Key, attr.Value.Any())
		}
		r.Attrs(func(attr slog.Attr) bool {
			fmt.Fprintf(&b, " %s=%v", attr.Key, attr.Value.Any())
			return true
		})
		message = b.String()
	}
	a.emit(NewLog(level, message))
	return nil
}

func (a *jobSlogAdapter) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := append([]slog.Attr{}, a.attrs...)
	merged = append(merged, attrs...)
	return &jobSlogAdapter{emit: a.emit, level: a.level, attrs: merged}
}

func (a *jobSlogAdapter) WithGroup(_ string) slog.Handler {
	// Adapter ignores groups; group nesting collapses into the message.
	return a
}

// slogLevelToLogLevel maps slog severity to rediver LogLevel.
func slogLevelToLogLevel(l slog.Level) LogLevel {
	switch {
	case l < slog.LevelInfo:
		return LogLevelDebug
	case l < slog.LevelWarn:
		return LogLevelInfo
	case l < slog.LevelError:
		return LogLevelWarn
	default:
		return LogLevelError
	}
}
