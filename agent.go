package rediver

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/califio/rediver-sdk-go/internal/auth"
	"github.com/califio/rediver-sdk-go/internal/transport"
	"github.com/califio/rediver-sdk-go/internal/worker"
)

// Agent is the per-scanner agent. Create with NewAgent; start with Run, RunOnce, or Dispatch.
type Agent struct {
	scanner     Scanner
	scannerName string       // scanner name in DB (normalized)
	agentToken  string       // opaque rat- token, set in NewAgent, shared by all runners of this agent
	serverURL   string       // set in NewAgent
	runnerID    string       // set after RegisterMachine; identifies this runner machine
	config      *agentConfig // shared config (read-only after creation)

	tokenManager *auth.TokenManager // per-agent token lifecycle
	client       *transport.Client  // per-agent Connect client
	pool         *worker.Pool       // per-agent worker pool
	retrier      *retrier
	logger       *slog.Logger

	// testPollDoer overrides client for pollLoop in unit tests; nil in production.
	testPollDoer pollDoer

	// drainCtx survives graceful shutdown so in-flight jobs can finish
	drainCtx    context.Context
	cancelDrain context.CancelFunc

	mu      sync.Mutex  // guards cancelDrain
	running atomic.Bool // one-shot guard
}

// NewAgent creates an Agent for a single scanner. Token is resolved from the
// argument or the REDIVER_TOKEN env var. Server URL defaults to
// DefaultServerURL but can be overridden via WithServerURL or REDIVER_URL.
//
// NewAgent validates configuration and builds non-network state. Token
// generation happens inside the chosen lifecycle method (Run, RunOnce,
// Dispatch) with the appropriate persistence flag.
func NewAgent(token string, scanner Scanner, opts ...Option) (*Agent, error) {
	if scanner == nil {
		return nil, fmt.Errorf("%w: scanner is required", ErrInvalidConfig)
	}

	cfg := defaultAgentConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	tok := resolveAgentToken(token)
	if tok == "" {
		return nil, fmt.Errorf("%w: agent token is required (set REDIVER_TOKEN or pass as argument)", ErrInvalidConfig)
	}

	url := resolveServerURL(cfg.serverURL)

	a := &Agent{
		scanner:     scanner,
		scannerName: strings.ToLower(scanner.Name()),
		config:      cfg,
		agentToken:  tok,
		serverURL:   url,
		logger:      cfg.logger.With("scanner", strings.ToLower(scanner.Name())),
		retrier:     newRetrier(cfg.retryPolicy),
	}
	return a, nil
}

// Stop cancels in-flight work. Optional — ctx cancellation passed to Run* is
// the primary stop signal. Idempotent.
func (a *Agent) Stop() {
	a.mu.Lock()
	cancel := a.cancelDrain
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
