package rediver

import (
	"log/slog"
	"sync"
	"testing"
	"time"
)

type fakeLogRPC struct {
	mu    sync.Mutex
	calls []rpcLogCall
}

type rpcLogCall struct {
	Level   LogLevel
	Message string
}

func (f *fakeLogRPC) record(level LogLevel, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, rpcLogCall{Level: level, Message: message})
}

func (f *fakeLogRPC) getCalls() []rpcLogCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]rpcLogCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func waitForCalls(f *fakeLogRPC, n int) []rpcLogCall {
	for range 200 {
		if calls := f.getCalls(); len(calls) >= n {
			return calls
		}
		time.Sleep(time.Millisecond)
	}
	return f.getCalls()
}

func TestLogger_SendsToBackendViaSink(t *testing.T) {
	rpc := &fakeLogRPC{}
	j := &job{
		logFn:    rpc.record,
		logLevel: slog.LevelInfo,
	}

	logger := j.Logger()
	logger.Info("starting scan", "target", "example.com")
	logger.Error("scan failed", "reason", "timeout")

	calls := waitForCalls(rpc, 2)
	if len(calls) != 2 {
		t.Fatalf("got %d RPC calls, want 2", len(calls))
	}

	if calls[0].Level != LogLevelInfo {
		t.Errorf("call[0].Level = %q, want %q", calls[0].Level, LogLevelInfo)
	}
	if calls[0].Message != "starting scan target=example.com" {
		t.Errorf("call[0].Message = %q", calls[0].Message)
	}

	if calls[1].Level != LogLevelError {
		t.Errorf("call[1].Level = %q, want %q", calls[1].Level, LogLevelError)
	}
	if calls[1].Message != "scan failed reason=timeout" {
		t.Errorf("call[1].Message = %q", calls[1].Message)
	}
}

func TestLogger_RespectsLogLevel(t *testing.T) {
	rpc := &fakeLogRPC{}
	j := &job{
		logFn:    rpc.record,
		logLevel: slog.LevelWarn,
	}

	logger := j.Logger()
	logger.Debug("debug msg")
	logger.Info("info msg")
	logger.Warn("warn msg")
	logger.Error("error msg")

	calls := waitForCalls(rpc, 2)
	if len(calls) != 2 {
		t.Fatalf("got %d RPC calls, want 2 (warn+error only)", len(calls))
	}
	if calls[0].Level != LogLevelWarn {
		t.Errorf("call[0].Level = %q, want warn", calls[0].Level)
	}
	if calls[1].Level != LogLevelError {
		t.Errorf("call[1].Level = %q, want error", calls[1].Level)
	}
}

func TestLogger_NilLogFn_NoBackendCalls(t *testing.T) {
	j := &job{
		logFn:    nil,
		logLevel: slog.LevelInfo,
	}

	logger := j.Logger()
	logger.Info("should not crash")
	logger.Error("also fine")
}

func TestLogger_WithAttrs_IncludedInMessage(t *testing.T) {
	rpc := &fakeLogRPC{}
	j := &job{
		logFn:    rpc.record,
		logLevel: slog.LevelInfo,
	}

	logger := j.Logger().With("scanner", "subfinder")
	logger.Info("found subdomain", "domain", "api.example.com")

	calls := waitForCalls(rpc, 1)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	msg := calls[0].Message
	if msg != "found subdomain scanner=subfinder domain=api.example.com" {
		t.Errorf("message = %q", msg)
	}
}

func TestSlogLevelToLogLevel(t *testing.T) {
	tests := []struct {
		input slog.Level
		want  LogLevel
	}{
		{slog.LevelDebug, LogLevelDebug},
		{slog.LevelInfo, LogLevelInfo},
		{slog.LevelWarn, LogLevelWarn},
		{slog.LevelError, LogLevelError},
		{slog.LevelDebug - 1, LogLevelDebug},
	}
	for _, tt := range tests {
		got := slogLevelToLogLevel(tt.input)
		if got != tt.want {
			t.Errorf("slogLevelToLogLevel(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
