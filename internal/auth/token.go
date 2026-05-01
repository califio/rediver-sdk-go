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
	RunModeWorker     RunMode = iota // long-running poll loop
	RunModeTask                      // single job, revoke token, exit
	RunModeCI                        // CI mode: detect env, create job, scan local repo, exit
	RunModeDispatcher                // dispatch mode: poll jobs, forward to external handler
)

// String returns the API enum value for this run mode.
func (m RunMode) String() string {
	switch m {
	case RunModeWorker, RunModeDispatcher:
		return "Worker"
	case RunModeTask, RunModeCI:
		return "Task"
	default:
		return "Worker"
	}
}

// GenerateTokenRequest is the data sent to the GenerateToken RPC.
// Per-scanner token exchange: cluster token + single scanner → agent token.
type GenerateTokenRequest struct {
	ClusterToken string
	Scanner      string // single scanner name
	Persistent   bool   // true=worker, false=task/CI
	Hostname     string
	IPAddress    string
	Version      string
	JobId        *string // nullable; set only for direct task execution
	AgentId      *string // nullable; set after first generate-token for 401 refresh
}

// GenerateTokenResponse holds the result of a generate-token call.
type GenerateTokenResponse struct {
	AgentId *string
	Token   *string
}

// GenerateTokenFunc is the function that performs per-scanner token exchange.
type GenerateTokenFunc func(ctx context.Context, req GenerateTokenRequest) (*GenerateTokenResponse, error)

// RevokeFunc is the function that revokes the agent token.
type RevokeFunc func(ctx context.Context, token string) error

// TokenManager manages the 2-token auth lifecycle.
// All modes use generate-token for initial acquisition and 401 refresh.
type TokenManager struct {
	clusterToken string
	agentToken   atomic.Value // stores string
	agentID      string
	mu           sync.Mutex
	revokeFn     RevokeFunc
	generateFn   GenerateTokenFunc    // for 401 refresh
	genReq       GenerateTokenRequest // cached request for 401 refresh
}

// NewTokenManager creates a TokenManager for 2-token auth.
func NewTokenManager(clusterToken string) *TokenManager {
	tm := &TokenManager{
		clusterToken: clusterToken,
	}
	tm.agentToken.Store("")
	return tm
}

// SetRevokeFunc sets the function that revokes the agent token.
func (tm *TokenManager) SetRevokeFunc(fn RevokeFunc) {
	tm.revokeFn = fn
}

// SetGenerateTokenFunc sets the function used for token refresh on 401.
// Called by transport.Client during initialization to avoid circular dependency.
func (tm *TokenManager) SetGenerateTokenFunc(fn GenerateTokenFunc) {
	tm.generateFn = fn
}

// GenerateToken refreshes the agent token using generate-token. Thread-safe, single-flight.
// Returns nil if another goroutine already refreshed successfully (token changed).
// Used by all modes on 401; network/5xx errors are returned for retry; 4xx errors are fatal.
func (tm *TokenManager) GenerateToken(ctx context.Context) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.generateFn == nil {
		return fmt.Errorf("generate-token function not set")
	}

	resp, err := tm.generateFn(ctx, tm.genReq)
	if err != nil {
		return fmt.Errorf("generate token: %w", err)
	}

	tm.agentToken.Store(derefStr(resp.Token))
	tm.agentID = derefStr(resp.AgentId)
	return nil
}

// SetToken stores an agent token directly (used after generate-token).
func (tm *TokenManager) SetToken(token string) {
	tm.agentToken.Store(token)
}

// SetAgentID stores the agent ID (used after generate-token in Task/CI modes).
func (tm *TokenManager) SetAgentID(id string) {
	tm.agentID = id
}

// SetGenReq caches the GenerateTokenRequest for 401 refresh in Task/CI modes.
func (tm *TokenManager) SetGenReq(req GenerateTokenRequest) {
	tm.genReq = req
}

// AgentToken returns the current agent token for X-Token header.
func (tm *TokenManager) AgentToken() string {
	return tm.agentToken.Load().(string)
}

// ClusterToken returns the cluster token (for generate-token requests only).
func (tm *TokenManager) ClusterToken() string {
	return tm.clusterToken
}

// AgentID returns the current agent ID.
func (tm *TokenManager) AgentID() string {
	return tm.agentID
}

// RevokeToken calls the revoke endpoint (task/CI mode shutdown).
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

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
