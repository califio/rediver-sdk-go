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
	"github.com/califio/rediver-sdk-go/utils"
)

// Agent is the per-scanner agent. Create with NewAgent; start with Run, RunOnce, RunCI, or Dispatch.
type Agent struct {
	scanner      Scanner
	scannerName  string        // scanner name in DB (normalized)
	clusterToken string        // set in NewAgent, used by lifecycle methods to gen token
	serverURL    string        // set in NewAgent
	agentID      string        // set after token gen
	config       *runnerConfig // shared from Runner (read-only after creation)
	token        atomic.Value  // stores string — current agent token

	tokenManager *auth.TokenManager // per-agent token lifecycle
	client       *transport.Client  // per-agent Connect client
	pool         *worker.Pool       // per-agent worker pool
	retrier      *retrier
	logger       *slog.Logger

	// testPollDoer overrides client for pollLoop in unit tests; nil in production.
	testPollDoer pollDoer

	genReq auth.GenerateTokenRequest // cached for 401 refresh

	// drainCtx survives graceful shutdown so in-flight jobs can finish
	drainCtx    context.Context
	cancelDrain context.CancelFunc

	mu      sync.Mutex  // guards cancelDrain + token refresh
	running atomic.Bool // one-shot guard (used in Phase 3)
}

// newAgentInternal creates a fully initialized agent by generating a token for the given scanner.
// persistent=true for Worker/Dispatcher modes, false for Task/CI modes.
// Used by Runner.createAgents (legacy path). Removed in Phase 5.
func newAgentInternal(ctx context.Context, s Scanner, clusterToken, serverURL string, persistent, syncMetadata bool, config *runnerConfig) (*Agent, error) {
	hostname := config.hostname
	if hostname == "" {
		hostname = utils.GetIPAddress()
	}

	scannerName := strings.ToLower(s.Name())

	genReq := auth.GenerateTokenRequest{
		ClusterToken: clusterToken,
		Scanner:      scannerName,
		Persistent:   persistent,
		Hostname:     hostname,
		IPAddress:    utils.GetIPAddress(),
		Version:      config.version,
	}
	if config.runMode == RunModeTask && config.directJobID != "" {
		genReq.JobId = &config.directJobID
	}

	tm := auth.NewTokenManager(clusterToken)
	tm.SetGenReq(genReq)

	client, err := transport.NewClient(serverURL, tm, config.httpClient)
	if err != nil {
		return nil, fmt.Errorf("create transport: %w", err)
	}

	// Generate initial token with retry
	ret := newRetrier(config.retryPolicy)
	var resp *auth.GenerateTokenResponse
	if err := ret.Do(ctx, func() error {
		var genErr error
		resp, genErr = client.DoGenerateToken(ctx, genReq)
		return genErr
	}); err != nil {
		return nil, fmt.Errorf("generate-token: %w", err)
	}

	agentID := derefStr(resp.AgentId)
	genReq.AgentId = &agentID
	tm.SetGenReq(genReq)
	token := derefStr(resp.Token)
	tm.SetToken(token)
	tm.SetAgentID(agentID)

	a := &Agent{
		scanner:      s,
		scannerName:  scannerName,
		agentID:      agentID,
		config:       config,
		tokenManager: tm,
		client:       client,
		retrier:      ret,
		genReq:       genReq,
		logger: config.logger.With(
			"scanner", scannerName,
			"agent_id", agentID,
		),
	}
	a.token.Store(token)
	a.pool = worker.NewPool(config.maxConcurrency, config.maxConcurrency*2)

	if syncMetadata {
		a.syncScannerMetadata(ctx)
	}

	config.logger.Info("agent initialized",
		"scanner", scannerName,
		"agent_id", agentID,
		"persistent", persistent,
	)

	return a, nil
}

// NewAgent creates an Agent for a single scanner. Token is resolved from the
// argument or the REDIVER_TOKEN env var. Server URL defaults to
// DefaultServerURL but can be overridden via WithServerURL or REDIVER_URL.
//
// NewAgent validates configuration and builds non-network state. Token
// generation happens inside the chosen lifecycle method (Run, RunOnce, RunCI,
// Dispatch) with the appropriate persistence flag.
func NewAgent(token string, scanner Scanner, opts ...Option) (*Agent, error) {
	if scanner == nil {
		return nil, fmt.Errorf("%w: scanner is required", ErrInvalidConfig)
	}

	cfg := defaultAgentConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	tok := resolveClusterToken(token)
	if tok == "" {
		return nil, fmt.Errorf("%w: cluster token is required (set REDIVER_TOKEN or pass as argument)", ErrInvalidConfig)
	}

	url := resolveServerURL(cfg.serverURL)

	a := &Agent{
		scanner:      scanner,
		scannerName:  strings.ToLower(scanner.Name()),
		config:       cfg,
		clusterToken: tok,
		serverURL:    url,
		logger:       cfg.logger.With("scanner", strings.ToLower(scanner.Name())),
		retrier:      newRetrier(cfg.retryPolicy),
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
