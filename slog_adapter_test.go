package rediver

import (
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestSlogAdapter_EmitsLogEvent(t *testing.T) {
	var got []Event
	adapter := newJobSlogAdapter(func(e Event) { got = append(got, e) }, slog.LevelDebug)
	logger := slog.New(adapter)

	logger.Info("scanning", "target", "mblife.vn", "count", 17)

	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	if got[0].Type != EventLog {
		t.Errorf("type: got %v", got[0].Type)
	}
	p := got[0].Payload.(LogPayload)
	if p.Level != LogLevelInfo {
		t.Errorf("level: got %v", p.Level)
	}
	if !strings.Contains(p.Message, "scanning") || !strings.Contains(p.Message, "target=mblife.vn") || !strings.Contains(p.Message, "count=17") {
		t.Errorf("message lost kv: got %q", p.Message)
	}
}

func TestSlogAdapter_RespectsLevelFilter(t *testing.T) {
	var got []Event
	adapter := newJobSlogAdapter(func(e Event) { got = append(got, e) }, slog.LevelWarn)
	logger := slog.New(adapter)
	logger.Info("filtered out")
	logger.Warn("kept")
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	if got[0].Payload.(LogPayload).Message != "kept" {
		t.Errorf("got %q", got[0].Payload.(LogPayload).Message)
	}
}

func TestSlogLevelToLogLevel(t *testing.T) {
	cases := map[slog.Level]LogLevel{
		slog.LevelDebug: LogLevelDebug,
		slog.LevelInfo:  LogLevelInfo,
		slog.LevelWarn:  LogLevelWarn,
		slog.LevelError: LogLevelError,
	}
	for in, want := range cases {
		if got := slogLevelToLogLevel(in); got != want {
			t.Errorf("%v: got %v, want %v", in, got, want)
		}
	}
	_ = context.Background
}
