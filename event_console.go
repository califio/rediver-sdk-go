package rediver

import (
	"context"
	"fmt"
	"log/slog"
)

const consolePreviewLen = 80

// formatEventForConsole returns the slog.Level + message + attrs to write for
// the given event. Returns ok=false when the event should be skipped (e.g.,
// no payload). slog's own level filter then decides whether the formatted
// record is actually emitted.
func formatEventForConsole(ev Event) (level slog.Level, msg string, attrs []slog.Attr, ok bool) {
	switch ev.Type {
	case EventLog:
		p, _ := ev.Payload.(LogPayload)
		return logLevelToSlog(p.Level), p.Message, nil, true

	case EventToolUseStart:
		p, _ := ev.Payload.(ToolUseStartPayload)
		return slog.LevelInfo, "tool_use_start",
			[]slog.Attr{slog.String("tool", p.Name), slog.String("id", p.ToolID)}, true

	case EventToolUseEnd:
		p, _ := ev.Payload.(ToolUseEndPayload)
		status := "ok"
		if p.IsError {
			status = "error"
		}
		return slog.LevelInfo, "tool_use_end",
			[]slog.Attr{slog.String("id", p.ToolID), slog.String("status", status)}, true

	case EventTextEnd:
		p, _ := ev.Payload.(TextPayload)
		return slog.LevelInfo, "text",
			[]slog.Attr{
				slog.Int("len", len(p.Text)),
				slog.String("preview", preview(p.Text, consolePreviewLen)),
			}, true

	case EventThinkingEnd:
		p, _ := ev.Payload.(TextPayload)
		return slog.LevelInfo, "thinking",
			[]slog.Attr{
				slog.Int("len", len(p.Text)),
				slog.String("preview", preview(p.Text, consolePreviewLen)),
			}, true

	case EventTextDelta, EventThinkingDelta:
		// Deltas at debug level — typically filtered out at default Info threshold.
		p, _ := ev.Payload.(TextPayload)
		return slog.LevelDebug, string(ev.Type), []slog.Attr{slog.String("text", p.Text)}, true
	}
	return 0, "", nil, false
}

func logLevelToSlog(l LogLevel) slog.Level {
	switch l {
	case LogLevelDebug:
		return slog.LevelDebug
	case LogLevelWarn:
		return slog.LevelWarn
	case LogLevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func preview(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// emitConsole writes one console line for the event using the supplied logger.
// slog's level filter governs whether the line is actually rendered.
func emitConsole(ctx context.Context, logger *slog.Logger, ev Event) {
	level, msg, attrs, ok := formatEventForConsole(ev)
	if !ok {
		return
	}
	if !logger.Enabled(ctx, level) {
		return
	}
	r := slog.NewRecord(ev.Timestamp, level, msg, 0)
	r.AddAttrs(attrs...)
	_ = logger.Handler().Handle(ctx, r)
}

// Avoid unused-import lint when only fmt-based debugging is removed.
var _ = fmt.Sprintf
