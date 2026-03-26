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
	// RunModeDispatcher polls jobs and forwards them to an external handler.
	RunModeDispatcher = auth.RunModeDispatcher
)

// runnerConfig holds Runner/Agent shared configuration.
type runnerConfig struct {
	logger          *slog.Logger
	httpClient      *http.Client
	retryPolicy     RetryPolicy
	runMode         RunMode
	maxConcurrency  int
	pollInterval    time.Duration
	version         string
	hostname        string
	shutdownTimeout time.Duration
	repoDir         string // override repository directory for CI mode
	jobHandler      JobHandler
	directJobID     string // if set, skip poll and execute this job directly (RunDirect)
}

func defaultAgentConfig() *runnerConfig {
	hostname, _ := os.Hostname()
	return &runnerConfig{
		logger:          slog.Default(),
		httpClient:      &http.Client{Timeout: 30 * time.Second},
		retryPolicy:     DefaultRetryPolicy(),
		runMode:         resolveRunMode(),
		maxConcurrency:  1,
		pollInterval:    5 * time.Second,
		shutdownTimeout: 0, // wait forever
		hostname:        hostname,
	}
}

// resolveRunMode checks REDIVER_RUN_MODE env var. Default: task.
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

// Option configures a Runner or Agent.
type Option func(*runnerConfig)

// RunnerOption is an alias for Option.
type RunnerOption = Option

// WithWorkerMode sets the runner to worker mode (long-running poll loop).
func WithWorkerMode() Option {
	return func(c *runnerConfig) {
		c.runMode = RunModeWorker
	}
}

// WithTaskMode sets the runner to task mode (single job, revoke token, exit).
func WithTaskMode() Option {
	return func(c *runnerConfig) {
		c.runMode = RunModeTask
	}
}

// WithCIMode sets the runner to CI mode (detect env, create job, scan local repo, exit).
func WithCIMode() Option {
	return func(c *runnerConfig) {
		c.runMode = RunModeCI
	}
}

// WithMaxConcurrency sets the maximum number of concurrent jobs per scanner.
// Default is 1.
func WithMaxConcurrency(n int) Option {
	return func(c *runnerConfig) {
		if n > 0 {
			c.maxConcurrency = n
		}
	}
}

// WithPollInterval sets the polling interval. Default: 5s, min: 1s.
func WithPollInterval(d time.Duration) Option {
	return func(c *runnerConfig) {
		if d >= 1*time.Second {
			c.pollInterval = d
		}
	}
}

// WithVersion sets the agent version string for token generation.
func WithVersion(v string) Option {
	return func(c *runnerConfig) {
		c.version = v
	}
}

// WithHostname sets the hostname sent during token generation.
func WithHostname(h string) Option {
	return func(c *runnerConfig) {
		c.hostname = h
	}
}

// WithLogger sets a custom slog logger.
func WithLogger(logger *slog.Logger) Option {
	return func(c *runnerConfig) {
		if logger != nil {
			c.logger = logger
		}
	}
}

// WithRetry sets the retry policy.
func WithRetry(policy RetryPolicy) Option {
	return func(c *runnerConfig) {
		c.retryPolicy = policy
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(c *runnerConfig) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// WithShutdownTimeout sets graceful shutdown timeout. 0 = wait forever (default).
func WithShutdownTimeout(d time.Duration) Option {
	return func(c *runnerConfig) {
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
func WithRepoDir(path string) Option {
	return func(c *runnerConfig) {
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
func WithJobID(id string) RunOption {
	return func(c *runConfig) {
		c.jobID = id
	}
}
