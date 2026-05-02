package rediver

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// DispatchMode controls how PollJob requests are issued.
type DispatchMode string

const (
	// DispatchPolling is the legacy mode: client-side ticker, server returns
	// immediately. Default.
	DispatchPolling DispatchMode = "polling"
	// DispatchLongPolling: client sends wait_seconds; server holds the request
	// until a job is available or the deadline is hit. No client-side sleep
	// between calls. Recommended for low-latency dispatch.
	DispatchLongPolling DispatchMode = "long-polling"
)

const (
	defaultLongPollWait = 30 * time.Second
	maxLongPollWait     = 60 * time.Second

	// DefaultServerURL is the default Rediver API server URL.
	DefaultServerURL = "https://api.rediver.ai"
)

// agentConfig holds Agent configuration.
type agentConfig struct {
	logger                 *slog.Logger
	httpClient             *http.Client
	retryPolicy            RetryPolicy
	maxConcurrency         int
	pollInterval           time.Duration
	dispatchMode           DispatchMode
	longPollWait           time.Duration
	version                string
	hostname               string
	shutdownTimeout        time.Duration
	repoDir                string // override repository directory for CI mode
	syncMetadataDispatcher bool   // allow Dispatcher mode to sync scanner metadata on startup
	serverURL              string // override server URL; empty → resolveServerURL() picks env or default
}

// dispatchParams returns (waitSeconds, clientSleep) used by the poll loop.
//
//	polling:      (0, pollInterval) — server returns immediately, client sleeps.
//	long-polling: (longPollWait_seconds, 0) — server holds, client doesn't sleep.
func (c *agentConfig) dispatchParams() (int32, time.Duration) {
	switch c.dispatchMode {
	case DispatchLongPolling:
		return int32(c.longPollWait.Seconds()), 0
	default:
		return 0, c.pollInterval
	}
}

func defaultAgentConfig() *agentConfig {
	hostname, _ := os.Hostname()
	return &agentConfig{
		logger: slog.Default(),
		// 90s default accommodates long-poll mode (server holds up to 60s,
		// SDK wraps ctx with waitSeconds+5s = 65s) without HTTP client timing
		// out before the ctx deadline. Short-poll mode is unaffected — empty
		// PollJob returns immediately, well under 90s.
		httpClient:      &http.Client{Timeout: 90 * time.Second},
		retryPolicy:     DefaultRetryPolicy(),
		maxConcurrency:  1,
		pollInterval:    5 * time.Second,
		dispatchMode:    DispatchPolling,
		longPollWait:    defaultLongPollWait,
		shutdownTimeout: 0, // wait forever
		hostname:        hostname,
	}
}

// Option configures an Agent.
type Option func(*agentConfig)


// WithDispatcherMetadataSync enables scanner metadata sync when running in Dispatcher mode.
// Disabled by default because not every dispatcher owns scanner configuration in the backend.
func WithDispatcherMetadataSync() Option {
	return func(c *agentConfig) {
		c.syncMetadataDispatcher = true
	}
}

// WithMaxConcurrency sets the maximum number of concurrent jobs per scanner.
// Default is 1.
func WithMaxConcurrency(n int) Option {
	return func(c *agentConfig) {
		if n > 0 {
			c.maxConcurrency = n
		}
	}
}

// WithPollInterval sets the polling interval. Default: 5s, min: 1s.
// Has no effect in DispatchLongPolling mode (server holds the request instead).
func WithPollInterval(d time.Duration) Option {
	return func(c *agentConfig) {
		if d >= 1*time.Second {
			c.pollInterval = d
		}
	}
}

// WithDispatchMode selects how PollJob requests are issued. Default is
// DispatchPolling (legacy short-poll). DispatchLongPolling has the server
// hold the request until a job is available or wait timeout — much lower
// dispatch latency, no client-side sleep between calls.
func WithDispatchMode(m DispatchMode) Option {
	return func(c *agentConfig) {
		switch m {
		case DispatchPolling, DispatchLongPolling:
			c.dispatchMode = m
		default:
			panic(fmt.Sprintf("rediver: invalid dispatch mode %q", m))
		}
	}
}

// WithLongPollWait sets the server-side hold duration for long-polling mode.
// Default 30s. Must be in (0, 60s]. No effect in polling mode.
func WithLongPollWait(d time.Duration) Option {
	return func(c *agentConfig) {
		if d > 0 && d <= maxLongPollWait {
			c.longPollWait = d
		}
	}
}

// WithVersion sets the agent version string for token generation.
func WithVersion(v string) Option {
	return func(c *agentConfig) {
		c.version = v
	}
}

// WithHostname sets the hostname sent during token generation.
func WithHostname(h string) Option {
	return func(c *agentConfig) {
		c.hostname = h
	}
}

// WithLogger sets a custom slog logger.
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
func WithRepoDir(path string) Option {
	return func(c *agentConfig) {
		c.repoDir = path
	}
}

// WithNoRetry disables retry (fail on first error).
func WithNoRetry() Option {
	return WithRetry(NoRetry())
}

// WithServerURL sets the Rediver API server URL. Overrides REDIVER_URL env.
// Default: DefaultServerURL ("https://api.rediver.ai").
func WithServerURL(url string) Option {
	return func(c *agentConfig) {
		c.serverURL = url
	}
}

// resolveClusterToken resolves the cluster token from the argument or REDIVER_TOKEN env.
func resolveClusterToken(explicit string) string {
	if explicit != "" {
		return explicit
	}
	return strings.TrimSpace(os.Getenv("REDIVER_TOKEN"))
}

// resolveServerURL returns the server URL using priority:
// explicit (option/arg) → REDIVER_URL env → DefaultServerURL.
func resolveServerURL(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if env := strings.TrimSpace(os.Getenv("REDIVER_URL")); env != "" {
		return env
	}
	return DefaultServerURL
}
