package rediver

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func newCaptureLogger(level slog.Level) (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	h := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: level})
	return slog.New(h), buf
}

func TestFormatEventForConsole_Log(t *testing.T) {
	ev := NewLog(LogLevelInfo, "hello")
	level, msg, _, ok := formatEventForConsole(ev)
	if !ok {
		t.Fatal("ok=false")
	}
	if level != slog.LevelInfo {
		t.Errorf("level: got %v", level)
	}
	if msg != "hello" {
		t.Errorf("msg: got %q", msg)
	}
}

func TestEmitConsole_LevelFilter_DefaultInfoSilencesDeltas(t *testing.T) {
	logger, buf := newCaptureLogger(slog.LevelInfo)
	ev := NewTextDelta("chunk")
	ev.Timestamp = time.Now()
	emitConsole(context.Background(), logger, ev)
	if buf.Len() != 0 {
		t.Errorf("expected no output at LevelInfo for delta, got %q", buf.String())
	}
}

func TestEmitConsole_DebugLevel_PrintsDeltas(t *testing.T) {
	logger, buf := newCaptureLogger(slog.LevelDebug)
	ev := NewTextDelta("chunk")
	ev.Timestamp = time.Now()
	emitConsole(context.Background(), logger, ev)
	if !strings.Contains(buf.String(), "text.delta") {
		t.Errorf("expected text.delta in output, got %q", buf.String())
	}
}

func TestEmitConsole_TextEnd_PrintsSummary(t *testing.T) {
	logger, buf := newCaptureLogger(slog.LevelInfo)
	long := strings.Repeat("a", 200)
	ev := NewTextEnd(long)
	ev.Timestamp = time.Now()
	emitConsole(context.Background(), logger, ev)
	out := buf.String()
	if !strings.Contains(out, "len=200") {
		t.Errorf("missing len=200 in %q", out)
	}
	if strings.Count(out, "a") > 90 {
		t.Errorf("body should be truncated, got %d a's", strings.Count(out, "a"))
	}
}

func TestEmitConsole_NilHandlerSafe(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ev := NewLog(LogLevelInfo, "x")
	ev.Timestamp = time.Now()
	emitConsole(context.Background(), logger, ev) // must not panic
}
