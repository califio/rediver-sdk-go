package rediver

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	agentv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/agent/v1"
)

func (a *Agent) executeJob(ctx context.Context, jobID string) error {
	a.logger.Info("executing job", "job_id", jobID)

	detail, err := a.getJobDetail(ctx, jobID)
	if err != nil {
		a.reportJobFailed(ctx, jobID, fmt.Sprintf("get detail: %v", err))
		return err
	}

	j := newJob(detail)
	j.(*job).artifactDownloadFn = a.client.GetArtifactPresignedURL
	j.(*job).executionToken = a.tokenManager.AgentToken()

	scannerName := a.scannerName
	if detail.Scanner != "" {
		scannerName = strings.ToLower(detail.Scanner)
	}
	jobLogger := a.config.logger.With("job_id", jobID, "scanner", scannerName)
	j.(*job).logger = jobLogger

	eventCtx, cancelEvents := context.WithCancel(ctx)
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
	a.reportJobStarted(ctx, jobID)

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
		if err := j.(*job).prepareRepository(ctx); err != nil {
			a.reportJobFailed(ctx, jobID, fmt.Sprintf("prepare repo: %v", err))
			cancelEvents()
			eventWg.Wait()
			return err
		}
		defer j.(*job).cleanupRepository()
	}

	hbCtx, cancelHB := context.WithCancel(ctx)
	go a.jobHeartbeatLoop(hbCtx, jobID)

	var resolvedHeadSHA string
	if jImpl, ok := j.(*job); ok {
		resolvedHeadSHA = jImpl.resolvedHeadSHA
	}
	scanErr := a.scanner.Scan(ctx, j, func(res Result) {
		a.importResult(ctx, jobID, res, resolvedHeadSHA)
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
		a.reportJobFailed(ctx, jobID, scanErr.Error())
		return scanErr
	}
	a.reportJobCompleted(ctx, jobID)
	return nil
}

func (a *Agent) getJobDetail(ctx context.Context, jobID string) (*agentv1.GetJobDetailResponse, error) {
	var detail *agentv1.GetJobDetailResponse
	err := a.retrier.Do(ctx, func() error {
		var err error
		detail, err = a.client.GetJobDetail(ctx, jobID)
		return err
	})
	return detail, err
}

func (a *Agent) reportJobStarted(ctx context.Context, jobID string) {
	_ = a.client.JobStart(ctx, jobID)
}

func (a *Agent) reportJobCompleted(ctx context.Context, jobID string) {
	_ = a.client.JobCompleted(ctx, jobID)
}

func (a *Agent) reportJobFailed(ctx context.Context, jobID string, description string) {
	_ = a.client.JobFailed(ctx, jobID, description)
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
