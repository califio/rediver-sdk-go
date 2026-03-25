package rediver

import (
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/califio/rediver-sdk-go/internal/auth"
)


// RunMode determines the agent execution mode.
type RunMode = auth.RunMode

const (
	// RunModeWorker is the default long-running poll loop mode.
	RunModeWorker = auth.RunModeWorker
	// RunModeTask executes a single job, revokes token, and exits.
	RunModeTask = auth.RunModeTask
	// RunModeCI detects CI environment, creates a job, scans local repo, and exits.
	RunModeCI = auth.RunModeCI
)

// agentConfig holds Agent configuration.
type agentConfig struct {
	logger          *slog.Logger
	httpClient      *http.Client
	retryPolicy     RetryPolicy
	runMode         RunMode
	maxConcurrency  int
	pollInterval    time.Duration
	version         string
	hostname        string
	agentID         string // force specific agent ID
	agentIDPath     string // file path for persistence (daemon only)
	shutdownTimeout time.Duration
	repoDir         string // override repository directory for CI mode
}

func defaultAgentConfig() *agentConfig {
	hostname, _ := os.Hostname()
	return &agentConfig{
		logger:          slog.Default(),
		httpClient:      &http.Client{Timeout: 30 * time.Second},
		retryPolicy:     DefaultRetryPolicy(),
		runMode:         resolveRunMode(),
		maxConcurrency:  1,
		pollInterval:    5 * time.Second,
		agentIDPath:     auth.DefaultAgentIDPath(),
		shutdownTimeout: 0, // wait forever
		hostname:        hostname,
	}
}

// resolveRunMode checks REDIVER_RUN_MODE env var. Default: daemon.
func resolveRunMode() RunMode {
	mode := strings.ToLower(os.Getenv("REDIVER_RUN_MODE"))
	switch mode {
	case "worker":
		return RunModeWorker
	case "task":
		return RunModeTask
	case "ci":
		return RunModeCI
	default:
		return RunModeTask
	}
}

// Option configures an Agent.
type Option func(*agentConfig)

// WithWorkerMode sets the agent to worker mode (long-running poll loop).
func WithWorkerMode() Option {
	return func(c *agentConfig) {
		c.runMode = RunModeWorker
	}
}

// WithTaskMode sets the agent to task mode (single job, revoke token, exit).
func WithTaskMode() Option {
	return func(c *agentConfig) {
		c.runMode = RunModeTask
	}
}

// WithCIMode sets the agent to CI mode (detect env, create job, scan local repo, exit).
func WithCIMode() Option {
	return func(c *agentConfig) {
		c.runMode = RunModeCI
	}
}

// WithMaxConcurrency sets the maximum number of concurrent jobs.
// Only applies to daemon mode. Default is 1.
func WithMaxConcurrency(n int) Option {
	return func(c *agentConfig) {
		if n > 0 {
			c.maxConcurrency = n
		}
	}
}

// WithPollInterval sets the polling interval. Default: 5s, min: 1s.
func WithPollInterval(d time.Duration) Option {
	return func(c *agentConfig) {
		if d >= 1*time.Second {
			c.pollInterval = d
		}
	}
}

// WithAgentID forces a specific agent ID instead of using cached/generated.
// Deprecated: Per-scanner agents use generate-token which assigns agent IDs server-side.
// This option is a no-op and will be removed in a future version.
func WithAgentID(id string) Option {
	return func(c *agentConfig) {
		c.agentID = id
	}
}

// WithAgentIDPath sets the file path for agent ID persistence (daemon only).
// Deprecated: Per-scanner agents no longer persist agent IDs to disk.
// This option is a no-op and will be removed in a future version.
func WithAgentIDPath(path string) Option {
	return func(c *agentConfig) {
		c.agentIDPath = path
	}
}

// WithVersion sets the agent version string for registration.
func WithVersion(v string) Option {
	return func(c *agentConfig) {
		c.version = v
	}
}

// WithHostname sets the hostname for registration.
func WithHostname(h string) Option {
	return func(c *agentConfig) {
		c.hostname = h
	}
}

// WithLogger sets a custom slog logger for agent-level logging.
func WithLogger(logger *slog.Logger) Option {
	return func(c *agentConfig) {
		if logger != nil {
			c.logger = logger
		}
	}
}

// WithRetry sets the retry policy.
func WithRetry(policy RetryPolicy) Option {
	return func(c *agentConfig) {
		c.retryPolicy = policy
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(c *agentConfig) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// WithShutdownTimeout sets graceful shutdown timeout. 0 = wait forever (default).
func WithShutdownTimeout(d time.Duration) Option {
	return func(c *agentConfig) {
		c.shutdownTimeout = d
	}
}

// WithRetryDefault uses the default retry policy.
func WithRetryDefault() Option {
	return WithRetry(DefaultRetryPolicy())
}

// WithRetryAggressive uses an aggressive retry policy with more attempts.
func WithRetryAggressive() Option {
	return WithRetry(AggressiveRetryPolicy())
}

// WithRepoDir overrides the repository directory for CI mode scanning.
// When set, the agent uses this path instead of auto-detected CI workspace or CWD.
func WithRepoDir(path string) Option {
	return func(c *agentConfig) {
		c.repoDir = path
	}
}

// WithNoRetry disables retry (fail on first error).
func WithNoRetry() Option {
	return WithRetry(NoRetry())
}

// RunOption configures a single Run() invocation.
type RunOption func(*runConfig)

type runConfig struct {
	jobID string // if set, skip pull and execute this job directly
}

// WithJobID skips job polling and executes the specified job directly.
// The agent validates it has a capable scanner before execution.
func WithJobID(id string) RunOption {
	return func(c *runConfig) {
		c.jobID = id
	}
}
