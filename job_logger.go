package rediver

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// jobLogHandler is a slog.Handler that fans out log records to two sinks:
//   - Terminal: writes structured text to stderr
//   - Backend: sends level+message via Log RPC (fire-and-forget)
type jobLogHandler struct {
	logFn    logFunc
	minLevel slog.Level
	console  *slog.TextHandler
	attrs    []slog.Attr
	groups   []string
}

func newJobLogHandler(logFn logFunc, level slog.Level) *jobLogHandler {
	return &jobLogHandler{
		logFn:    logFn,
		minLevel: level,
		console:  slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}),
	}
}

func (h *jobLogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.minLevel
}

func (h *jobLogHandler) Handle(ctx context.Context, r slog.Record) error {
	_ = h.console.Handle(ctx, r)

	if h.logFn != nil {
		level := slogLevelToLogLevel(r.Level)
		msg := formatLogRecord(r, h.groups, h.attrs)
		go h.logFn(level, msg)
	}
	return nil
}

func (h *jobLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &jobLogHandler{
		logFn:    h.logFn,
		minLevel: h.minLevel,
		console:  h.console.WithAttrs(attrs).(*slog.TextHandler),
		attrs:    append(h.attrs[:len(h.attrs):len(h.attrs)], attrs...),
		groups:   h.groups,
	}
}

func (h *jobLogHandler) WithGroup(name string) slog.Handler {
	return &jobLogHandler{
		logFn:    h.logFn,
		minLevel: h.minLevel,
		console:  h.console.WithGroup(name).(*slog.TextHandler),
		attrs:    h.attrs,
		groups:   append(h.groups[:len(h.groups):len(h.groups)], name),
	}
}

func slogLevelToLogLevel(l slog.Level) LogLevel {
	switch {
	case l >= slog.LevelError:
		return LogLevelError
	case l >= slog.LevelWarn:
		return LogLevelWarn
	case l >= slog.LevelInfo:
		return LogLevelInfo
	default:
		return LogLevelDebug
	}
}

// formatLogRecord builds a flat "message key=value key=value" string for the Log RPC.
func formatLogRecord(r slog.Record, groups []string, preAttrs []slog.Attr) string {
	var b strings.Builder
	b.WriteString(r.Message)

	writeAttr := func(a slog.Attr) {
		if a.Key == "" {
			return
		}
		b.WriteByte(' ')
		for _, g := range groups {
			b.WriteString(g)
			b.WriteByte('.')
		}
		b.WriteString(a.Key)
		b.WriteByte('=')
		b.WriteString(fmt.Sprint(a.Value.Any()))
	}

	for _, a := range preAttrs {
		writeAttr(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		writeAttr(a)
		return true
	})

	return b.String()
}
