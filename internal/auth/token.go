package auth

import (
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

// TokenManager holds the agent's persistent auth token plus the runner machine
// ID set after RegisterMachine. The per-job JWT (jobToken) is NOT stored here
// — it is passed per-call via context (transport.WithJobToken) so concurrent
// jobs each carry their own token without shared mutable state.
//
// Agent token is config-supplied (passed to NewTokenManager). It is persistent
// and managed by the user; SDK never revokes it. Job tokens auto-invalidate
// server-side: JobCompleted/JobFailed handlers delete the JTI from token
// cache, rendering the JWT unusable for further calls.
type TokenManager struct {
	agentToken atomic.Value // string
	runnerID   atomic.Value // string
}

// NewTokenManager constructs a TokenManager seeded with the agent token
// supplied by the SDK config.
func NewTokenManager(agentToken string) *TokenManager {
	tm := &TokenManager{}
	tm.agentToken.Store(agentToken)
	tm.runnerID.Store("")
	return tm
}

func (tm *TokenManager) AgentToken() string         { return tm.agentToken.Load().(string) }
func (tm *TokenManager) SetAgentToken(token string) { tm.agentToken.Store(token) }

func (tm *TokenManager) RunnerID() string       { return tm.runnerID.Load().(string) }
func (tm *TokenManager) SetRunnerID(id string)  { tm.runnerID.Store(id) }
