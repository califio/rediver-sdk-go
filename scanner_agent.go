package rediver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/califio/rediver-sdk-go/internal/auth"
	"github.com/califio/rediver-sdk-go/internal/transport"
	"github.com/califio/rediver-sdk-go/internal/worker"
)

// scannerAgent manages a single scanner's lifecycle: token, heartbeat, poll, workers.
// Each registered scanner gets its own scannerAgent with independent state.
type scannerAgent struct {
	scanner     Scanner
	scannerName string // scanner name in DB
	agentID     string
	config      *agentConfig
	token       atomic.Value // stores string (agent token)
	clusterInfo auth.ClusterInfo
	system      bool // true if cluster is system (no tenant)

	// Per-scanner token manager and HTTP client for authenticated API calls
	tokenManager *auth.TokenManager
	client       *transport.Client

	// Generate-token request cached for token refresh
	genReq auth.GenerateTokenRequest

	parentAgent *Agent // reference to parent for job execution
	pool        *worker.Pool
	retrier     *retrier
	logger      *slog.Logger

	// drainCtx survives graceful shutdown so running jobs can finish
	drainCtx    context.Context
	cancelDrain context.CancelFunc

	mu sync.Mutex // guards token refresh
}

// newScannerAgent creates a scannerAgent from a generate-token response.
func newScannerAgent(
	parent *Agent,
	s Scanner,
	resp *auth.GenerateTokenResponse,
	genReq auth.GenerateTokenRequest,
	config *agentConfig,
	parentClient *transport.Client,
) (*scannerAgent, error) {
	// Use request scanner name — always available regardless of backend response
	scannerName := genReq.Scanner
	sa := &scannerAgent{
		parentAgent: parent,
		scanner:     s,
		scannerName: scannerName,
		agentID:     resp.AgentID,
		config:      config,
		clusterInfo: resp.ClusterInfo,
		system:      resp.Scanner.System,
		genReq:      genReq,
		retrier:     newRetrier(config.retryPolicy),
		logger: config.logger.With(
			"scanner", scannerName,
			"agent_id", resp.AgentID,
		),
	}
	sa.token.Store(resp.Token)

	// Create per-scanner transport client with own token.
	// TokenManager's agentToken is read by the request editor for X-Token header.
	tm := auth.NewTokenManager(genReq.ClusterToken)
	tm.SetToken(resp.Token)
	client, err := transport.NewClient(parentClient.BaseURL(), tm, config.httpClient)
	if err != nil {
		return nil, fmt.Errorf("create scanner transport: %w", err)
	}
	sa.client = client
	sa.tokenManager = tm

	// Create worker pool (maxConcurrency applies per scanner)
	sa.pool = worker.NewPool(config.maxConcurrency, config.maxConcurrency*2)

	return sa, nil
}

// run starts the scannerAgent lifecycle: heartbeat + poll loops.
// Blocks until ctx is cancelled, then gracefully drains in-flight jobs.
func (sa *scannerAgent) run(ctx context.Context) error {
	sa.drainCtx, sa.cancelDrain = context.WithCancel(context.Background())
	defer sa.cancelDrain()

	sa.logger.Info("scanner-agent started")

	// Heartbeat loop (GET /api/agent/heartbeat)
	go sa.heartbeatLoop(sa.drainCtx)

	// Poll loop
	go sa.pollLoop(ctx)

	// Wait for cancellation
	<-ctx.Done()
	sa.logger.Info("scanner-agent shutting down")

	// Graceful: wait for in-flight jobs
	done := make(chan struct{})
	go func() {
		sa.pool.Shutdown()
		close(done)
	}()

	if sa.config.shutdownTimeout > 0 {
		select {
		case <-done:
			sa.logger.Info("all jobs completed")
		case <-time.After(sa.config.shutdownTimeout):
			sa.logger.Warn("shutdown timeout, forcing exit")
			sa.cancelDrain()
			sa.pool.ShutdownNow()
		}
	} else {
		<-done
		sa.logger.Info("all jobs completed")
	}

	sa.cancelDrain()
	return nil // controlled shutdown → return nil so errgroup doesn't cancel others
}

// heartbeatLoop sends GET /api/agent/heartbeat every 60s.
func (sa *scannerAgent) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(agentHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := sa.client.AgentHeartbeat(ctx); err != nil {
				if strings.Contains(err.Error(), "401") {
					if refreshErr := sa.refreshToken(ctx); refreshErr != nil {
						sa.logger.Error("heartbeat token refresh failed", "error", refreshErr)
						continue
					}
					if retryErr := sa.client.AgentHeartbeat(ctx); retryErr != nil {
						sa.logger.Warn("heartbeat retry failed", "error", retryErr)
					}
				} else {
					sa.logger.Warn("heartbeat failed", "error", err)
				}
			}
		}
	}
}

// pollLoop pulls jobs and dispatches to the worker pool.
func (sa *scannerAgent) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(sa.config.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sa.pollAndDispatch(ctx)
		}
	}
}

// pollAndDispatch pulls a job and submits it to the worker pool.
func (sa *scannerAgent) pollAndDispatch(ctx context.Context) {
	if sa.pool.ActiveWorkers() >= sa.config.maxConcurrency {
		return
	}

	jobID, err := sa.pullJob(ctx)
	if err != nil {
		if !errors.Is(err, ErrNoJobAvailable) {
			sa.logger.Error("pull job failed", "error", err)
		}
		return
	}

	err = sa.pool.Submit(&scannerAgentJob{
		sa:    sa,
		ctx:   sa.drainCtx,
		jobID: jobID,
	})
	if err != nil {
		sa.logger.Error("submit job failed", "job_id", jobID, "error", err)
		sa.reportJobFailed(ctx, jobID, "worker pool full")
	}
}

// pullJob requests a job from the server with 401 retry.
func (sa *scannerAgent) pullJob(ctx context.Context) (string, error) {
	res, err := sa.client.RequestJobWithResponse(ctx)
	if err != nil {
		return "", err
	}

	// 401 → refresh token and retry once
	if res.StatusCode() == 401 {
		if refreshErr := sa.refreshToken(ctx); refreshErr != nil {
			return "", fmt.Errorf("token refresh after 401: %w", refreshErr)
		}
		sa.logger.Info("token refreshed after 401")
		res, err = sa.client.RequestJobWithResponse(ctx)
		if err != nil {
			return "", err
		}
	}

	if res.StatusCode() == 204 {
		return "", ErrNoJobAvailable
	}
	if res.StatusCode() >= 400 {
		return "", &APIError{StatusCode: res.StatusCode(), Response: string(res.Body)}
	}
	if res.JSON200 == nil || res.JSON200.JobId == nil {
		return "", ErrNoJobAvailable
	}

	jobID := *res.JSON200.JobId
	if jobID == "" {
		return "", ErrNoJobAvailable
	}
	return jobID, nil
}

// refreshToken calls generate-token to get a new agent token.
// Thread-safe, single-flight via mutex.
func (sa *scannerAgent) refreshToken(ctx context.Context) error {
	sa.mu.Lock()
	defer sa.mu.Unlock()

	resp, err := sa.client.DoGenerateToken(ctx, sa.genReq)
	if err != nil {
		return err
	}

	sa.token.Store(resp.Token)
	sa.tokenManager.SetToken(resp.Token)
	sa.agentID = resp.AgentID
	sa.logger.Info("token refreshed", "agent_id", resp.AgentID)
	return nil
}

// reportJobFailed reports a job failure to the server via the parent agent.
func (sa *scannerAgent) reportJobFailed(ctx context.Context, jobID string, description string) {
	sa.parentAgent.reportJobFailed(ctx, jobID, description)
}

// --- Worker pool job wrapper for scannerAgent ---

type scannerAgentJob struct {
	sa    *scannerAgent
	ctx   context.Context // drainCtx
	jobID string
}

func (j *scannerAgentJob) Execute(_ context.Context) error {
	// Delegate to parent Agent's executeJob which uses the scanner registry.
	// Use drainCtx so running jobs can finish during graceful shutdown.
	return j.sa.parentAgent.executeJob(j.ctx, j.jobID)
}

func (j *scannerAgentJob) OnEnqueue() {
	j.sa.logger.Debug("job enqueued", "job_id", j.jobID)
}

func (j *scannerAgentJob) OnError(err error) {
	j.sa.logger.Error("job failed", "job_id", j.jobID, "error", err)
}

func (j *scannerAgentJob) OnCompleted() {
	j.sa.logger.Debug("job completed", "job_id", j.jobID)
}

