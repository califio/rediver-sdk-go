package rediver

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestDefaultAgentConfig(t *testing.T) {
	cfg := defaultAgentConfig()

	if cfg.httpClient.Timeout != 30*time.Second {
		t.Errorf("httpClient.Timeout: got %v, want 30s", cfg.httpClient.Timeout)
	}
	if cfg.maxConcurrency != 1 {
		t.Errorf("maxConcurrency: got %d, want 1", cfg.maxConcurrency)
	}
	if cfg.pollInterval != 5*time.Second {
		t.Errorf("pollInterval: got %v, want 5s", cfg.pollInterval)
	}
	if cfg.shutdownTimeout != 0 {
		t.Errorf("shutdownTimeout: got %v, want 0", cfg.shutdownTimeout)
	}
	if cfg.logger == nil {
		t.Error("logger should not be nil")
	}
	hostname, _ := os.Hostname()
	if cfg.hostname != hostname {
		t.Errorf("hostname: got %q, want %q", cfg.hostname, hostname)
	}
	if cfg.retryPolicy.MaxAttempts != 5 {
		t.Errorf("retryPolicy.MaxAttempts: got %d, want 5", cfg.retryPolicy.MaxAttempts)
	}
}

func TestWithWorkerMode(t *testing.T) {
	cfg := defaultAgentConfig()
	WithWorkerMode()(cfg)
	if cfg.runMode != RunModeWorker {
		t.Errorf("runMode: got %v, want RunModeWorker", cfg.runMode)
	}
}

func TestWithTaskMode(t *testing.T) {
	cfg := defaultAgentConfig()
	WithTaskMode()(cfg)
	if cfg.runMode != RunModeTask {
		t.Errorf("runMode: got %v, want RunModeTask", cfg.runMode)
	}
}

func TestWithCIMode(t *testing.T) {
	cfg := defaultAgentConfig()
	WithCIMode()(cfg)
	if cfg.runMode != RunModeCI {
		t.Errorf("runMode: got %v, want RunModeCI", cfg.runMode)
	}
}

func TestWithDispatcherMetadataSync(t *testing.T) {
	cfg := defaultAgentConfig()
	WithDispatcherMetadataSync()(cfg)
	if !cfg.syncMetadataDispatcher {
		t.Error("syncMetadataDispatcher should be true")
	}
}

func TestWithMaxConcurrency(t *testing.T) {
	cfg := defaultAgentConfig()

	WithMaxConcurrency(5)(cfg)
	if cfg.maxConcurrency != 5 {
		t.Errorf("got %d, want 5", cfg.maxConcurrency)
	}

	// n=0 should be ignored
	WithMaxConcurrency(0)(cfg)
	if cfg.maxConcurrency != 5 {
		t.Errorf("n=0 should be ignored, got %d", cfg.maxConcurrency)
	}

	// n=-1 should be ignored
	WithMaxConcurrency(-1)(cfg)
	if cfg.maxConcurrency != 5 {
		t.Errorf("n=-1 should be ignored, got %d", cfg.maxConcurrency)
	}
}

func TestWithPollInterval(t *testing.T) {
	cfg := defaultAgentConfig()

	WithPollInterval(10 * time.Second)(cfg)
	if cfg.pollInterval != 10*time.Second {
		t.Errorf("got %v, want 10s", cfg.pollInterval)
	}

	// Below 1s should be ignored
	WithPollInterval(500 * time.Millisecond)(cfg)
	if cfg.pollInterval != 10*time.Second {
		t.Errorf("<1s should be ignored, got %v", cfg.pollInterval)
	}
}

func TestWithVersion(t *testing.T) {
	cfg := defaultAgentConfig()
	WithVersion("1.2.0")(cfg)
	if cfg.version != "1.2.0" {
		t.Errorf("got %q, want 1.2.0", cfg.version)
	}
}

func TestWithHostname(t *testing.T) {
	cfg := defaultAgentConfig()
	WithHostname("scanner-1")(cfg)
	if cfg.hostname != "scanner-1" {
		t.Errorf("got %q, want scanner-1", cfg.hostname)
	}
}

func TestWithLogger_Valid(t *testing.T) {
	cfg := defaultAgentConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	WithLogger(logger)(cfg)
	if cfg.logger != logger {
		t.Error("logger should be set to custom logger")
	}
}

func TestWithLogger_Nil(t *testing.T) {
	cfg := defaultAgentConfig()
	original := cfg.logger
	WithLogger(nil)(cfg)
	if cfg.logger != original {
		t.Error("nil logger should be ignored")
	}
}

func TestWithRetry(t *testing.T) {
	cfg := defaultAgentConfig()
	policy := RetryPolicy{MaxAttempts: 99}
	WithRetry(policy)(cfg)
	if cfg.retryPolicy.MaxAttempts != 99 {
		t.Errorf("got %d, want 99", cfg.retryPolicy.MaxAttempts)
	}
}

func TestWithHTTPClient_Valid(t *testing.T) {
	cfg := defaultAgentConfig()
	client := &http.Client{Timeout: 60 * time.Second}
	WithHTTPClient(client)(cfg)
	if cfg.httpClient != client {
		t.Error("httpClient should be set to custom client")
	}
}

func TestWithHTTPClient_Nil(t *testing.T) {
	cfg := defaultAgentConfig()
	original := cfg.httpClient
	WithHTTPClient(nil)(cfg)
	if cfg.httpClient != original {
		t.Error("nil client should be ignored")
	}
}

func TestWithShutdownTimeout(t *testing.T) {
	cfg := defaultAgentConfig()
	WithShutdownTimeout(30 * time.Second)(cfg)
	if cfg.shutdownTimeout != 30*time.Second {
		t.Errorf("got %v, want 30s", cfg.shutdownTimeout)
	}
}

func TestWithRetryDefault(t *testing.T) {
	cfg := defaultAgentConfig()
	cfg.retryPolicy = NoRetry() // change first
	WithRetryDefault()(cfg)
	if cfg.retryPolicy.MaxAttempts != 5 {
		t.Errorf("got %d, want 5 (default)", cfg.retryPolicy.MaxAttempts)
	}
}

func TestWithRetryAggressive(t *testing.T) {
	cfg := defaultAgentConfig()
	WithRetryAggressive()(cfg)
	if cfg.retryPolicy.MaxAttempts != 10 {
		t.Errorf("got %d, want 10 (aggressive)", cfg.retryPolicy.MaxAttempts)
	}
}

func TestWithNoRetry(t *testing.T) {
	cfg := defaultAgentConfig()
	WithNoRetry()(cfg)
	if cfg.retryPolicy.MaxAttempts != 1 {
		t.Errorf("got %d, want 1 (no retry)", cfg.retryPolicy.MaxAttempts)
	}
}

func TestResolveRunMode(t *testing.T) {
	tests := []struct {
		env  string
		want RunMode
	}{
		{"worker", RunModeWorker},
		{"task", RunModeTask},
		{"ci", RunModeCI},
		{"WORKER", RunModeWorker}, // case insensitive
		{"", RunModeTask},         // default
		{"unknown", RunModeTask},  // unknown defaults to task
	}

	for _, tc := range tests {
		t.Run("mode="+tc.env, func(t *testing.T) {
			t.Setenv("REDIVER_RUN_MODE", tc.env)
			got := resolveRunMode()
			if got != tc.want {
				t.Errorf("resolveRunMode(%q): got %v, want %v", tc.env, got, tc.want)
			}
		})
	}
}

func TestWithJobID(t *testing.T) {
	cfg := &runConfig{}
	WithJobID("job-123")(cfg)
	if cfg.jobID != "job-123" {
		t.Errorf("got %q, want job-123", cfg.jobID)
	}
}

func TestMultipleOptions(t *testing.T) {
	cfg := defaultAgentConfig()
	opts := []Option{
		WithWorkerMode(),
		WithMaxConcurrency(4),
		WithPollInterval(10 * time.Second),
		WithVersion("2.0.0"),
		WithHostname("multi-test"),
	}
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.runMode != RunModeWorker {
		t.Error("runMode not set")
	}
	if cfg.maxConcurrency != 4 {
		t.Error("maxConcurrency not set")
	}
	if cfg.pollInterval != 10*time.Second {
		t.Error("pollInterval not set")
	}
	if cfg.version != "2.0.0" {
		t.Error("version not set")
	}
	if cfg.hostname != "multi-test" {
		t.Error("hostname not set")
	}
}
