package rediver

import (
	"context"
	"fmt"
	"os"
	"sync"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/califio/rediver-sdk-go/internal/transport"
)

// runCI runs CI mode: detect git context, create job, execute, revoke token, return.
func (a *Agent) runCI(ctx context.Context) error {
	ci := a.detectGitContext()
	if ci == nil {
		return fmt.Errorf("%w: not running in a recognized CI environment or git repository", ErrInvalidConfig)
	}

	a.logger.Info("CI environment detected",
		"provider", string(ci.Provider),
		"repo", ci.Repo.Name,
		"ref", ci.Ref.Name,
	)

	supportsRepo := false
	for _, t := range a.scanner.AssetTypes() {
		if t == TargetTypeRepository {
			supportsRepo = true
			break
		}
	}
	if !supportsRepo {
		return fmt.Errorf("%w: scanner %q does not support TargetTypeRepository", ErrInvalidConfig, a.scannerName)
	}

	return a.executeCIJob(ctx, ci)
}

func (a *Agent) detectGitContext() *CIContext {
	if a.config.repoDir != "" && !isCIEnvironment() {
		return detectLocalGit(a.config.repoDir)
	}
	ci := DetectGitContext()
	if a.config.repoDir != "" {
		if ci == nil {
			ci = detectLocalGit(a.config.repoDir)
		} else {
			ci.RepoDir = a.config.repoDir
		}
	}
	return ci
}

func isCIEnvironment() bool {
	return os.Getenv("GITLAB_CI") == "true" || os.Getenv("GITHUB_ACTIONS") == "true"
}

func (a *Agent) executeCIJob(ctx context.Context, ci *CIContext) error {
	params := resolveParamsFromEnv(a.scanner.Params(), ci.Parameters)

	apiReq := ciContextToProtoRequest(ci, a.scannerName)
	if len(params) > 0 {
		s, err := structpb.NewStruct(params)
		if err == nil {
			apiReq.Parameters = s
		}
	}

	ciResp, err := a.client.CreateCiJob(ctx, apiReq)
	if err != nil {
		return fmt.Errorf("create CI job: %w", err)
	}
	if !ciResp.GetSuccess() {
		msg := ciResp.GetMessage()
		if msg == "" {
			msg = "unknown error"
		}
		return fmt.Errorf("create CI job: %s", msg)
	}
	jobID := ciResp.GetJobId()
	if jobID == "" {
		return fmt.Errorf("create CI job: no job ID returned")
	}

	a.logger.Info("CI job created", "job_id", jobID)

	// Mint per-job JWT (agent-plane, X-Token). All subsequent job-scope calls
	// use jobCtx which carries this token via Authorization: Bearer.
	jobToken, err := a.client.CreateJobToken(ctx, jobID, a.tokenManager.RunnerID())
	if err != nil {
		return fmt.Errorf("create job token: %w", err)
	}
	jobCtx := transport.WithJobToken(ctx, jobToken)

	j := newCIJob(jobID, ci, a.scannerName, params)
	j.(*job).executionToken = jobToken

	ciJobLogger := a.config.logger.With("job_id", jobID, "scanner", a.scannerName)

	// Event transport runs with jobCtx so AppendJobEvents uses Bearer.
	ciEventCtx, cancelCIEvents := context.WithCancel(jobCtx)
	ciSender := &agentEventSender{client: a.client}
	ciTr := newEventTransport(jobID, ciSender, ciJobLogger, 0, 0, 0)
	j.(*job).transport = ciTr

	var ciEventWg sync.WaitGroup
	ciEventWg.Add(1)
	go func() {
		defer ciEventWg.Done()
		ciTr.Run(ciEventCtx)
	}()

	ciJobLogger.Info("job started", "ci_provider", string(ci.Provider))
	a.reportJobStarted(jobCtx, jobID)

	if err := j.(*job).prepareRepository(jobCtx); err != nil {
		a.reportJobFailed(jobCtx, jobID, fmt.Sprintf("prepare repo: %v", err))
		cancelCIEvents()
		ciEventWg.Wait()
		return err
	}
	defer j.(*job).cleanupRepository()

	// Heartbeat goroutine inherits jobCtx so it routes via Bearer.
	hbCtx, cancelHB := context.WithCancel(jobCtx)
	go a.jobHeartbeatLoop(hbCtx, jobID)

	scanErr := a.scanner.Scan(jobCtx, j, func(res Result) {
		a.importResult(jobCtx, jobID, res, "")
	})

	if scanErr != nil {
		ciJobLogger.Error("job failed", "error", scanErr.Error())
	} else {
		ciJobLogger.Info("job completed")
	}

	cancelCIEvents()
	ciEventWg.Wait()
	cancelHB()

	if scanErr != nil {
		a.reportJobFailed(jobCtx, jobID, scanErr.Error())
		return scanErr
	}
	a.reportJobCompleted(jobCtx, jobID)
	return nil
}

// RunCI runs CI mode: ephemeral token, detects git context, calls CreateCiJob,
// executes, revokes token, returns. Errors if scanner does not declare
// TargetTypeRepository.
//
// Returns ErrAlreadyRunning if this Agent has already started.
func (a *Agent) RunCI(ctx context.Context) error {
	if !a.running.CompareAndSwap(false, true) {
		return ErrAlreadyRunning
	}
	if err := a.initSession(ctx, false, false); err != nil {
		return err
	}
	return a.runCI(ctx)
}
