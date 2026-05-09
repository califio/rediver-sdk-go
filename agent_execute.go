package rediver

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	scannerv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/scanner/v1"
	"github.com/califio/rediver-sdk-go/internal/transport"
)

func (a *Agent) executeJob(ctx context.Context, jobID string) error {
	a.logger.Info("executing job", "job_id", jobID)

	// Mint a per-job JWT via agent-plane (X-Token). CreateJobToken is itself
	// an agent-scope RPC, so the plain ctx (no job token) is correct here.
	jobToken, err := a.client.CreateJobToken(ctx, jobID, a.tokenManager.RunnerID())
	if err != nil {
		a.reportJobFailed(ctx, jobID, fmt.Sprintf("create job token: %v", err))
		return err
	}

	// Derive a per-job context. All job-scope calls in this goroutine use
	// jobCtx so they route via Authorization: Bearer <jwt>. Concurrent jobs
	// in sibling goroutines have distinct jobCtx — no shared mutable state.
	jobCtx := transport.WithJobToken(ctx, jobToken)

	detail, err := a.getJobDetail(jobCtx)
	if err != nil {
		a.reportJobFailed(jobCtx, jobID, fmt.Sprintf("get detail: %v", err))
		return err
	}

	j := newJob(detail)
	j.(*job).artifactDownloadFn = func(dCtx context.Context, artifactID string) (*ArtifactDownload, error) {
		// Artifact download is an agent-plane call; use jobCtx which still
		// carries X-Token via fallback (no job-token on artifact service).
		info, err := a.client.GetArtifactDownload(dCtx, artifactID)
		if err != nil {
			return nil, err
		}
		return &ArtifactDownload{
			PresignedURL:        info.PresignedURL,
			EncryptionAlgorithm: info.EncryptionAlgorithm,
			EncryptionKey:       info.EncryptionKey,
		}, nil
	}
	j.(*job).executionToken = jobToken

	scannerName := a.scannerName
	if detail.Scanner != "" {
		scannerName = strings.ToLower(detail.Scanner)
	}
	jobLogger := a.config.logger.With("job_id", jobID, "scanner", scannerName)

	// Start event transport with jobCtx so AppendJobEvents routes via Bearer.
	eventCtx, cancelEvents := context.WithCancel(jobCtx)
	sender := &agentEventSender{client: a.client}
	tr := newEventTransport(jobID, sender, jobLogger, 0, 0, 0)
	j.(*job).transport = tr

	var eventWg sync.WaitGroup
	eventWg.Add(1)
	go func() {
		defer eventWg.Done()
		tr.Run(eventCtx)
	}()

	jobLogger.Info("job started", "scanner", scannerName)
	a.reportJobStarted(jobCtx, jobID)

	if repo, hasRepo := j.Repository(); hasRepo {
		attrs := []any{
			"url", repo.URL,
			"branch", repo.Branch,
			"commit", repo.CommitSHA,
			"base_commit", repo.BaseCommitSHA,
			"event", repo.Event,
		}
		if repo.ArtifactID != "" {
			attrs = append(attrs, "artifact_id", repo.ArtifactID)
		}
		jobLogger.Info("preparing repository", attrs...)
		if err := j.(*job).prepareRepository(jobCtx); err != nil {
			a.reportJobFailed(jobCtx, jobID, fmt.Sprintf("prepare repo: %v", err))
			cancelEvents()
			eventWg.Wait()
			return err
		}
		defer j.(*job).cleanupRepository()
	}

	// Spawn heartbeat goroutine with jobCtx so it also uses Bearer.
	hbCtx, cancelHB := context.WithCancel(jobCtx)
	go a.jobHeartbeatLoop(hbCtx, jobID)

	var resolvedHeadSHA string
	if jImpl, ok := j.(*job); ok {
		resolvedHeadSHA = jImpl.resolvedHeadSHA
	}
	scanErr := a.scanner.Scan(jobCtx, j, func(res Result) {
		a.importResult(jobCtx, jobID, res, resolvedHeadSHA)
	})

	if scanErr != nil {
		jobLogger.Error("job failed", "error", scanErr.Error())
	} else {
		jobLogger.Info("job completed")
	}

	cancelEvents()
	eventWg.Wait()
	cancelHB()

	if scanErr != nil {
		a.reportJobFailed(jobCtx, jobID, scanErr.Error())
		return scanErr
	}
	a.reportJobCompleted(jobCtx, jobID)
	return nil
}

func (a *Agent) getJobDetail(jobCtx context.Context) (*scannerv1.GetJobDetailResponse, error) {
	var detail *scannerv1.GetJobDetailResponse
	err := a.retrier.Do(jobCtx, func() error {
		var err error
		detail, err = a.client.GetJobDetail(jobCtx)
		return err
	})
	return detail, err
}

func (a *Agent) reportJobStarted(jobCtx context.Context, jobID string) {
	if err := a.client.JobStart(jobCtx); err != nil {
		a.logger.Warn("job start failed", "job_id", jobID, "error", err)
	}
}

func (a *Agent) reportJobCompleted(jobCtx context.Context, jobID string) {
	if err := a.client.JobCompleted(jobCtx); err != nil {
		a.logger.Warn("job completed report failed", "job_id", jobID, "error", err)
	}
}

func (a *Agent) reportJobFailed(jobCtx context.Context, jobID string, description string) {
	if err := a.client.JobFailed(jobCtx, description); err != nil {
		a.logger.Warn("job failed report failed", "job_id", jobID, "error", err)
	}
}

type agentPoolJob struct {
	a     *Agent
	ctx   context.Context
	jobID string
}

func (j *agentPoolJob) Execute(_ context.Context) error {
	return j.a.executeJob(j.ctx, j.jobID)
}

func (j *agentPoolJob) OnEnqueue() {
	j.a.logger.Debug("job enqueued", "job_id", j.jobID)
}

func (j *agentPoolJob) OnError(err error) {
	j.a.logger.Error("job failed", "job_id", j.jobID, "error", err)
}

func (j *agentPoolJob) OnCompleted() {
	j.a.logger.Debug("job completed", "job_id", j.jobID)
}

// resolveParamsFromEnv resolves scanner parameters from env vars, CI context, and defaults.
func resolveParamsFromEnv(params []Param, ciParams map[string]interface{}) map[string]interface{} {
	resolved := make(map[string]interface{})
	for _, p := range params {
		if p.envVar != "" {
			if val, ok := os.LookupEnv(p.envVar); ok {
				resolved[p.name] = val
				continue
			}
		}
		if ciParams != nil {
			if val, ok := ciParams[p.name]; ok {
				resolved[p.name] = val
				continue
			}
		}
		if p.defaultVal != nil {
			resolved[p.name] = p.defaultVal
		}
	}
	return resolved
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
