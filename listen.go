package rediver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/califio/rediver-sdk-go/internal/auth"
	"github.com/califio/rediver-sdk-go/utils"
	"golang.org/x/sync/errgroup"
)

// JobHandler is called when a job is pulled from the server.
// The handler receives the job ID and decides how to dispatch it
// (e.g., create a K8s Job, send to a queue, etc.).
// Return nil on success; returning error calls JobFailed so the server
// can re-queue immediately, then continues polling.
type JobHandler func(ctx context.Context, jobID string) error

// ListenForJobs registers the agent with the server, then continuously
// polls for jobs and invokes handler for each job received.
// It blocks until ctx is cancelled, then gracefully waits for in-flight
// handler calls to complete.
//
// The agent must be created with WithWorkerMode() for agent ID persistence.
// Relevant options: WithMaxConcurrency, WithPollInterval, WithShutdownTimeout.
func (a *Agent) ListenForJobs(ctx context.Context, handler JobHandler) error {
	if handler == nil {
		return fmt.Errorf("%w: handler must be non-nil", ErrInvalidConfig)
	}
	if len(a.scanners) == 0 {
		return fmt.Errorf("%w: at least one scanner must be registered", ErrInvalidConfig)
	}
	if !a.running.CompareAndSwap(false, true) {
		return fmt.Errorf("%w: agent already running", ErrInvalidConfig)
	}
	defer a.running.Store(false)

	// Warn if not in Worker mode — agent ID won't persist across restarts
	if a.config.runMode != RunModeWorker {
		a.config.logger.Warn("ListenForJobs called without WithWorkerMode(); agent ID will not persist across restarts")
	}

	// Force Worker mode for registration path
	a.config.runMode = RunModeWorker

	return a.runListenerWithScannerAgents(ctx, handler)
}

// runListenerWithScannerAgents creates per-scanner agents and runs listen-mode dispatch loops.
func (a *Agent) runListenerWithScannerAgents(ctx context.Context, handler JobHandler) error {
	persistent := true // ListenForJobs always uses worker mode
	hostname := a.config.hostname
	if hostname == "" {
		hostname = utils.GetIPAddress()
	}
	if hostname == "" {
		return fmt.Errorf("%w: hostname or IP required for worker mode", ErrInvalidConfig)
	}

	// Generate tokens for all scanners
	var scannerAgents []*scannerAgent
	for name, s := range a.scanners {
		genReq := auth.GenerateTokenRequest{
			ClusterToken: a.clusterToken,
			Scanner:      name,
			Persistent:   persistent,
			Hostname:     hostname,
			IPAddress:    utils.GetIPAddress(),
			Version:      a.config.version,
		}

		var resp *auth.GenerateTokenResponse
		if err := a.retrier.Do(ctx, func() error {
			var genErr error
			resp, genErr = a.client.DoGenerateToken(ctx, genReq)
			return genErr
		}); err != nil {
			return fmt.Errorf("failed to initialize scanner %s: %w", name, err)
		}

		// Update scanner metadata
		a.updateScannerMetadata(ctx, []auth.RegisteredScannerInfo{resp.Scanner}, resp.Scanner.System)

		sa, err := newScannerAgent(a, s, resp, genReq, a.config, a.client)
		if err != nil {
			return fmt.Errorf("create scanner-agent %s: %w", name, err)
		}
		scannerAgents = append(scannerAgents, sa)
	}

	a.config.logger.Info("all scanner-agent listeners initialized",
		"count", len(scannerAgents),
	)

	// Launch all scanner-agent listeners via errgroup
	g, gCtx := errgroup.WithContext(ctx)
	for _, sa := range scannerAgents {
		g.Go(func() error { return sa.runListener(gCtx, handler) })
	}
	return g.Wait()
}

// dispatchJob implements worker.Job for the listen-mode dispatch path.
// Instead of executing the job, it calls the user's handler to dispatch it.
type dispatchJob struct {
	agent   *Agent
	handler JobHandler
	ctx     context.Context // drainCtx — survives graceful shutdown
	jobID   string
}

func (j *dispatchJob) Execute(_ context.Context) error {
	// Ignore pool's ctx — use captured drainCtx for graceful shutdown support.
	// Same pattern as agentJob.Execute in agent.go.
	err := j.handler(j.ctx, j.jobID)
	if err != nil {
		// Only report failure for real dispatch errors, not shutdown cancellation.
		if j.ctx.Err() == nil {
			j.agent.reportJobFailed(j.ctx, j.jobID, err.Error())
		}
	}
	return err
}

func (j *dispatchJob) OnEnqueue() {
	j.agent.config.logger.Debug("dispatch enqueued", "job_id", j.jobID)
}

func (j *dispatchJob) OnError(err error) {
	// Log only — JobFailed already called in Execute if needed.
	j.agent.config.logger.Error("dispatch failed", "job_id", j.jobID, "error", err)
}

func (j *dispatchJob) OnCompleted() {
	j.agent.config.logger.Debug("dispatch completed", "job_id", j.jobID)
}

// listenPollAndDispatch pulls a job and submits it to the handler via the worker pool.
func (a *Agent) listenPollAndDispatch(ctx context.Context, drainCtx context.Context, handler JobHandler) {
	if a.pool.ActiveWorkers() >= a.config.maxConcurrency {
		return
	}

	jobID, err := a.pullJob(ctx)
	if err != nil {
		if !errors.Is(err, ErrNoJobAvailable) {
			a.config.logger.Error("pull job failed", "error", err)
		}
		return
	}

	err = a.pool.Submit(&dispatchJob{
		agent:   a,
		handler: handler,
		ctx:     drainCtx,
		jobID:   jobID,
	})
	if err != nil {
		a.config.logger.Error("submit dispatch failed", "job_id", jobID, "error", err)
		a.reportJobFailed(ctx, jobID, "dispatch queue full")
	}
}

// runListener is the main loop for ListenForJobs mode.
// Mirrors runDaemon but dispatches to handler instead of executing jobs.
func (a *Agent) runListener(ctx context.Context, handler JobHandler) error {
	drainCtx, cancelDrain := context.WithCancel(context.Background())
	defer cancelDrain()

	// Start heartbeat (uses drainCtx so it continues during graceful shutdown)
	go a.agentHeartbeatLoop(drainCtx)

	// Start poll loop
	go func() {
		ticker := time.NewTicker(a.config.pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.listenPollAndDispatch(ctx, drainCtx, handler)
			}
		}
	}()

	// Wait for cancellation
	<-ctx.Done()
	a.config.logger.Info("listener shutting down...")

	// Graceful shutdown — same pattern as runDaemon
	done := make(chan struct{})
	go func() {
		a.pool.Shutdown()
		close(done)
	}()

	if a.config.shutdownTimeout > 0 {
		select {
		case <-done:
			a.config.logger.Info("all dispatches completed")
		case <-time.After(a.config.shutdownTimeout):
			a.config.logger.Warn("shutdown timeout, forcing exit")
			cancelDrain()
			a.pool.ShutdownNow()
		}
	} else {
		<-done
		a.config.logger.Info("all dispatches completed")
	}

	cancelDrain()
	return nil
}
