package rediver

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNewJobLogger_DualHandler(t *testing.T) {
	var buf bytes.Buffer
	agentLogger := slog.New(slog.NewTextHandler(&buf, nil))

	logger, bufHandler := newJobLogger("job-123", "subdomain", agentLogger)
	logger.Info("scan started", "target", "example.com")

	// Verify buffer handler captured entry.
	entries := bufHandler.Drain()
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Message != "scan started" {
		t.Errorf("message = %q, want %q", e.Message, "scan started")
	}
	if e.JobID != "job-123" {
		t.Errorf("job_id = %q, want %q", e.JobID, "job-123")
	}
	if e.Scanner != "subdomain" {
		t.Errorf("scanner = %q, want %q", e.Scanner, "subdomain")
	}
	if e.Level != LogLevelInfo {
		t.Errorf("level = %v, want INFO", e.Level)
	}
	if e.Fields["target"] != "example.com" {
		t.Errorf("target = %v, want %q", e.Fields["target"], "example.com")
	}

	// Verify agent logger received log with job context attrs.
	out := buf.String()
	if !strings.Contains(out, "job_id=job-123") {
		t.Errorf("agent log missing job_id: %s", out)
	}
	if !strings.Contains(out, "scanner=subdomain") {
		t.Errorf("agent log missing scanner: %s", out)
	}
	if !strings.Contains(out, "target=example.com") {
		t.Errorf("agent log missing target: %s", out)
	}
}

func TestNewJobLogger_AllLevels(t *testing.T) {
	agentLogger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelDebug}))

	logger, bufHandler := newJobLogger("job-1", "probe", agentLogger)
	logger.Debug("d")
	logger.Info("i")
	logger.Warn("w")
	logger.Error("e")

	entries := bufHandler.Drain()
	if len(entries) != 4 {
		t.Fatalf("got %d entries, want 4", len(entries))
	}

	expected := []LogLevel{LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError}
	for i, e := range entries {
		if e.Level != expected[i] {
			t.Errorf("entry[%d] level = %v, want %v", i, e.Level, expected[i])
		}
	}
}

func TestJobBufferHandler_DrainClearsBuffer(t *testing.T) {
	agentLogger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	logger, bufHandler := newJobLogger("job-1", "s", agentLogger)

	logger.Info("msg1")
	logger.Info("msg2")

	entries := bufHandler.Drain()
	if len(entries) != 2 {
		t.Fatalf("first drain: got %d, want 2", len(entries))
	}

	entries = bufHandler.Drain()
	if len(entries) != 0 {
		t.Errorf("second drain: got %d, want 0", len(entries))
	}
}

func TestJobBufferHandler_MaxBufferDrop(t *testing.T) {
	buf := newJobBufferHandler("job-1", "s")
	buf.maxBuffer = 256 // tiny limit

	logger := slog.New(buf)
	// Fill buffer until entries get dropped.
	for i := 0; i < 100; i++ {
		logger.Info("this is a long message that should fill the buffer quickly")
	}

	entries := buf.Drain()
	if len(entries) >= 100 {
		t.Errorf("expected some entries to be dropped, got %d", len(entries))
	}
	if len(entries) == 0 {
		t.Error("expected at least some entries before buffer full")
	}
}

func TestJobBufferHandler_NoFields(t *testing.T) {
	agentLogger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	logger, bufHandler := newJobLogger("job-1", "s", agentLogger)

	logger.Info("no fields")

	entries := bufHandler.Drain()
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Fields != nil {
		t.Errorf("expected nil fields, got %v", entries[0].Fields)
	}
}
