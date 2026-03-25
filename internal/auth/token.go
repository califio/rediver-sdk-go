package auth

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// RunMode determines the agent execution mode.
type RunMode int

const (
	RunModeWorker RunMode = iota // long-running poll loop
	RunModeTask                  // single job, revoke token, exit
	RunModeCI                    // CI mode: detect env, create job, scan local repo, exit
)

// String returns the API enum value for this run mode.
func (m RunMode) String() string {
	switch m {
	case RunModeWorker:
		return "Worker"
	case RunModeTask, RunModeCI:
		return "Task"
	default:
		return "Worker"
	}
}

// RegistrationRequest is the data sent to POST /api/agent/register.
// Worker mode only — sends scanner names (no metadata).
type RegistrationRequest struct {
	Scanners   []string // scanner names only
	AgentID    string   // cached, may be empty
	Hostname   string
	IPAddress  string
	Version    string
	SdkVersion string
}

// RegisteredScannerInfo is returned by backend after registration or token exchange.
// RequestName is the original name sent by the agent; Name is the resolved name in the DB.
// SDK uses RequestName → Name mapping to remap internal scanner registry.
type RegisteredScannerInfo struct {
	Name         string                 // resolved scanner name in DB (e.g., "custom_semgrep")
	RequestName  string                 // original name from agent registration request (e.g., "semgrep")
	DisplayName  string                 // human-readable label from DB
	ParamsSchema map[string]interface{} // current JSON Schema in DB, nil if none
	System       bool                   // true if this is a system scanner
}

// RegistrationResponse is the data received from registration.
type RegistrationResponse struct {
	AgentID     string
	Token       string
	ExpiresAt   string
	ClusterInfo ClusterInfo
	System      bool                    // true if cluster is system (no tenant)
	Scanners    []RegisteredScannerInfo // all scanners resolved during registration
}

// ClusterInfo holds cluster metadata from registration.
type ClusterInfo struct {
	ID                 string
	Name               string
	AgentType          string
	Tags               []string
	AcceptUntaggedJobs bool
	MaxConcurrentJobs  int
}

// ConnectRequest is the data sent to POST /api/agent/token (lightweight token exchange).
type ConnectRequest struct {
	Scanners []string // scanner names only (no metadata)
}

// ConnectResponse is the data received from POST /api/agent/token.
type ConnectResponse struct {
	AgentID     string
	Token       string
	ExpiresAt   string
	ClusterInfo ClusterInfo
	System      bool                    // true if cluster is system (no tenant)
	Scanners    []RegisteredScannerInfo // populated when scanners sent in request (task mode)
}

// GenerateTokenRequest is the data sent to POST /api/agent/generate-token.
// Per-scanner token exchange: cluster token + single scanner → agent token.
type GenerateTokenRequest struct {
	ClusterToken string
	Scanner      string // single scanner name
	Persistent   bool   // true=worker, false=task/CI
	Hostname     string
	IPAddress    string
	Version      string
}

// GenerateTokenResponse is the data received from POST /api/agent/generate-token.
type GenerateTokenResponse struct {
	AgentID     string
	Token       string
	ExpiresAt   string
	Scanner     RegisteredScannerInfo // single scanner (not list)
	ClusterInfo ClusterInfo
}

// GenerateTokenFunc is the function that performs per-scanner token exchange.
type GenerateTokenFunc func(ctx context.Context, req GenerateTokenRequest) (*GenerateTokenResponse, error)

// HeartbeatPingFunc is the function that sends GET /api/agent/heartbeat (204 response).
type HeartbeatPingFunc func(ctx context.Context) error

// RegisterFunc is the function that performs the actual HTTP registration.
type RegisterFunc func(ctx context.Context, req RegistrationRequest) (*RegistrationResponse, error)

// ConnectFunc is the function that performs lightweight token exchange (CI/task mode).
type ConnectFunc func(ctx context.Context, req ConnectRequest) (*ConnectResponse, error)

// RevokeFunc is the function that revokes the agent token.
type RevokeFunc func(ctx context.Context, token string) error

// TokenManager manages the 2-token auth lifecycle.
type TokenManager struct {
	clusterToken string
	agentToken   atomic.Value // stores string
	agentID      string
	clusterInfo  ClusterInfo
	runMode      RunMode
	mu           sync.Mutex
	persist      *Persister // nil for task mode
	registerFn   RegisterFunc
	connectFn    ConnectFunc
	revokeFn     RevokeFunc
	regReq       RegistrationRequest // cached for re-registration
}

// NewTokenManager creates a TokenManager for 2-token auth.
// persist is nil for task mode (no file I/O).
func NewTokenManager(clusterToken string, runMode RunMode, persist *Persister) *TokenManager {
	tm := &TokenManager{
		clusterToken: clusterToken,
		runMode:      runMode,
		persist:      persist,
	}
	tm.agentToken.Store("")
	return tm
}

// SetRegisterFunc sets the function that performs the actual HTTP registration.
// Called by transport.Client during initialization to avoid circular dependency.
func (tm *TokenManager) SetRegisterFunc(fn RegisterFunc) {
	tm.registerFn = fn
}

// SetConnectFunc sets the function that performs lightweight token exchange.
func (tm *TokenManager) SetConnectFunc(fn ConnectFunc) {
	tm.connectFn = fn
}

// SetRevokeFunc sets the function that revokes the agent token.
func (tm *TokenManager) SetRevokeFunc(fn RevokeFunc) {
	tm.revokeFn = fn
}

// Register performs initial registration. Must be called once before any API calls.
// Returns the registration response for caller inspection (e.g., scanner name remapping).
func (tm *TokenManager) Register(ctx context.Context, req RegistrationRequest) (*RegistrationResponse, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.registerFn == nil {
		return nil, fmt.Errorf("register function not set")
	}

	// Read cached agent ID for daemon mode — only if no explicit ID set
	if tm.persist != nil && req.AgentID == "" {
		cachedID, err := tm.persist.ReadAgentID()
		if err == nil && cachedID != "" {
			req.AgentID = cachedID
		}
	}

	tm.regReq = req

	resp, err := tm.registerFn(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("register: %w", err)
	}

	tm.agentToken.Store(resp.Token)
	tm.agentID = resp.AgentID
	tm.clusterInfo = resp.ClusterInfo

	// Persist agent ID for daemon mode
	if tm.persist != nil && resp.AgentID != "" {
		if err := tm.persist.WriteAgentID(resp.AgentID); err != nil {
			// Non-fatal: log warning, proceed without persistence
			_ = err
		}
	}

	return resp, nil
}

// Connect performs lightweight token exchange (CI/task mode).
// No scanner metadata, no DB record — just exchanges cluster token for agent token.
func (tm *TokenManager) Connect(ctx context.Context, req ConnectRequest) (*ConnectResponse, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.connectFn == nil {
		return nil, fmt.Errorf("connect function not set")
	}

	resp, err := tm.connectFn(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	tm.agentToken.Store(resp.Token)
	tm.agentID = resp.AgentID
	tm.clusterInfo = resp.ClusterInfo

	return resp, nil
}

// Reregister re-registers with cluster token. Thread-safe, single-flight.
// oldToken is the token the caller had when it received 401.
// Returns nil if another goroutine already re-registered successfully.
// Worker mode uses registerFn (full registration), Task/CI mode uses connectFn (token exchange).
func (tm *TokenManager) Reregister(ctx context.Context, oldToken string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Check if token already changed (another goroutine re-registered)
	currentToken := tm.agentToken.Load().(string)
	if currentToken != oldToken {
		return nil // already re-registered
	}

	// Worker mode: full re-registration (creates/updates agent record in DB)
	if tm.runMode == RunModeWorker {
		if tm.registerFn == nil {
			return fmt.Errorf("register function not set")
		}
		resp, err := tm.registerFn(ctx, tm.regReq)
		if err != nil {
			return fmt.Errorf("re-register: %w", err)
		}
		tm.agentToken.Store(resp.Token)
		tm.agentID = resp.AgentID
		tm.clusterInfo = resp.ClusterInfo
		if tm.persist != nil && resp.AgentID != "" {
			_ = tm.persist.WriteAgentID(resp.AgentID)
		}
		return nil
	}

	// Task/CI mode: lightweight token exchange (no DB record)
	if tm.connectFn == nil {
		return fmt.Errorf("connect function not set")
	}
	resp, err := tm.connectFn(ctx, ConnectRequest{})
	if err != nil {
		return fmt.Errorf("re-connect: %w", err)
	}
	tm.agentToken.Store(resp.Token)
	tm.agentID = resp.AgentID
	tm.clusterInfo = resp.ClusterInfo
	return nil
}

// SetToken stores an agent token directly (used by per-scanner agents after generate-token).
func (tm *TokenManager) SetToken(token string) {
	tm.agentToken.Store(token)
}

// AgentToken returns the current agent token for X-Token header.
func (tm *TokenManager) AgentToken() string {
	return tm.agentToken.Load().(string)
}

// ClusterToken returns the cluster token (for registration only).
func (tm *TokenManager) ClusterToken() string {
	return tm.clusterToken
}

// AgentID returns the current agent ID.
func (tm *TokenManager) AgentID() string {
	return tm.agentID
}

// GetClusterInfo returns cluster metadata from last registration.
func (tm *TokenManager) GetClusterInfo() ClusterInfo {
	return tm.clusterInfo
}

// RevokeToken calls POST /api/agent/token/revoke (task mode shutdown).
func (tm *TokenManager) RevokeToken(ctx context.Context) error {
	if tm.revokeFn == nil {
		return nil
	}
	token := tm.AgentToken()
	if token == "" {
		return nil
	}
	return tm.revokeFn(ctx, token)
}
