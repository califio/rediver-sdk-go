package rediver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/califio/rediver-sdk-go/internal/api"
	"github.com/califio/rediver-sdk-go/internal/auth"
	"github.com/califio/rediver-sdk-go/internal/transport"
	"github.com/califio/rediver-sdk-go/internal/worker"
	"github.com/califio/rediver-sdk-go/utils"
)

const (
	agentHeartbeatInterval = 60 * time.Second
	jobHeartbeatInterval   = 60 * time.Second
	agentMaxBatchSize      = 500
)

// agent is the internal per-scanner agent (unexported).
// It manages token generation, heartbeat, polling, and job execution for one scanner.
type agent struct {
	scanner      Scanner
	scannerName  string          // scanner name in DB (normalized)
	agentID      string          // from generate-token response
	config       *runnerConfig   // shared from Runner (read-only after creation)
	token        atomic.Value    // stores string — current agent token

	tokenManager *auth.TokenManager  // per-agent token lifecycle
	client       *transport.Client   // per-agent HTTP client
	pool         *worker.Pool        // per-agent worker pool
	retrier      *retrier
	logger       *slog.Logger

	genReq       auth.GenerateTokenRequest // cached for 401 refresh

	// drainCtx survives graceful shutdown so in-flight jobs can finish
	drainCtx    context.Context
	cancelDrain context.CancelFunc

	mu sync.Mutex // guards token refresh
}

// newAgent creates a fully initialized agent by generating a token for the given scanner.
// persistent=true for Worker/Dispatcher modes, false for Task/CI modes.
func newAgent(ctx context.Context, s Scanner, clusterToken, serverURL string, persistent bool, config *runnerConfig) (*agent, error) {
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

	// Create token manager (no persister — agent IDs are server-managed)
	tm := auth.NewTokenManager(clusterToken)
	tm.SetGenReq(genReq)

	// Create transport client — wires generateTokenFn and revokeFn into TokenManager
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

	// Cache AgentId in genReq for subsequent 401 refreshes
	agentID := derefStr(resp.AgentId)
	genReq.AgentId = &agentID
	tm.SetGenReq(genReq)
	token := derefStr(resp.Token)
	tm.SetToken(token)
	tm.SetAgentID(agentID)

	a := &agent{
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

	// Sync scanner metadata to backend for Worker/Dispatcher modes
	if persistent {
		a.syncScannerMetadata(ctx)
	}

	config.logger.Info("agent initialized",
		"scanner", scannerName,
		"agent_id", agentID,
		"persistent", persistent,
	)

	return a, nil
}

// --- Run modes ---

// run starts the Worker mode lifecycle: heartbeat + poll loops.
// Blocks until ctx is cancelled, then drains in-flight jobs.
func (a *agent) run(ctx context.Context) error {
	a.drainCtx, a.cancelDrain = context.WithCancel(context.Background())
	defer a.cancelDrain()

	a.logger.Info("agent started (worker mode)")

	go a.heartbeatLoop(a.drainCtx)
	go a.pollLoop(ctx)

	<-ctx.Done()
	a.logger.Info("agent shutting down")

	done := make(chan struct{})
	go func() {
		a.pool.Shutdown()
		close(done)
	}()

	if a.config.shutdownTimeout > 0 {
		select {
		case <-done:
			a.logger.Info("all jobs completed")
		case <-time.After(a.config.shutdownTimeout):
			a.logger.Warn("shutdown timeout, forcing exit")
			a.cancelDrain()
			a.pool.ShutdownNow()
		}
	} else {
		<-done
		a.logger.Info("all jobs completed")
	}

	a.cancelDrain()
	return nil // controlled shutdown — nil so errgroup doesn't cancel sibling agents
}

// runOnce runs Task mode: poll one job, execute it, revoke token, return.
func (a *agent) runOnce(ctx context.Context) error {
	jobID, _, err := a.pullJob(ctx)
	if errors.Is(err, ErrNoJobAvailable) {
		a.logger.Info("no job available, exiting")
		_ = a.tokenManager.RevokeToken(ctx)
		return nil
	}
	if err != nil {
		_ = a.tokenManager.RevokeToken(ctx)
		return err
	}

	execErr := a.executeJob(ctx, jobID)
	_ = a.tokenManager.RevokeToken(ctx)
	return execErr
}

// runCI runs CI mode: detect git context, create job, execute, revoke token, return.
func (a *agent) runCI(ctx context.Context) error {
	ci := a.detectGitContext()
	if ci == nil {
		_ = a.tokenManager.RevokeToken(ctx)
		return fmt.Errorf("%w: not running in a recognized CI environment or git repository", ErrInvalidConfig)
	}

	a.logger.Info("CI environment detected",
		"provider", string(ci.Provider),
		"repo", ci.Repo.Name,
		"ref", ci.Ref.Name,
	)

	// Only execute if this scanner supports TargetTypeRepository
	supportsRepo := false
	for _, t := range a.scanner.AssetTypes() {
		if t == TargetTypeRepository {
			supportsRepo = true
			break
		}
	}
	if !supportsRepo {
		_ = a.tokenManager.RevokeToken(ctx)
		return fmt.Errorf("%w: scanner %q does not support TargetTypeRepository", ErrInvalidConfig, a.scannerName)
	}

	err := a.executeCIJob(ctx, ci)
	_ = a.tokenManager.RevokeToken(ctx)
	return err
}

// runDispatcher runs Dispatcher mode: heartbeat + poll loops, calling handler instead of executing.
// Agent does NOT manage job lifecycle — external worker handles execution.
func (a *agent) runDispatcher(ctx context.Context, handler JobHandler) error {
	a.drainCtx, a.cancelDrain = context.WithCancel(context.Background())
	defer a.cancelDrain()

	a.logger.Info("agent started (dispatcher mode)")

	go a.heartbeatLoop(a.drainCtx)

	// Poll + dispatch loop
	ticker := time.NewTicker(a.config.pollInterval)
	defer ticker.Stop()

	var wg sync.WaitGroup
	for {
		select {
		case <-ctx.Done():
			a.logger.Info("dispatcher shutting down, waiting for handlers")
			wg.Wait()
			a.cancelDrain()
			return nil
		case <-ticker.C:
			jobID, scanner, err := a.pullJob(ctx)
			if err != nil {
				if !errors.Is(err, ErrNoJobAvailable) {
					a.logger.Error("pull job failed", "error", err)
					if errors.Is(err, ErrClusterRevoked) {
						return err
					}
				}
				continue
			}
			if jobID == "" {
				continue
			}

			pulled := PulledJob{JobID: jobID, Scanner: scanner}
			wg.Add(1)
			go func() {
				defer wg.Done()
				if herr := handler(ctx, pulled); herr != nil {
					a.logger.Error("dispatch handler failed", "job_id", jobID, "error", herr)
				}
			}()
		}
	}
}

// --- Heartbeat ---

// heartbeatLoop sends GET /api/agent/heartbeat every 60s.
// 401 retry is handled transparently by the transport layer.
func (a *agent) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(agentHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.client.AgentHeartbeat(ctx); err != nil {
				a.logger.Warn("heartbeat failed", "error", err)
			}
		}
	}
}

// --- Poll loop ---

// pollLoop drives the Worker poll-and-dispatch cycle.
func (a *agent) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(a.config.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.pollAndDispatch(ctx)
		}
	}
}

// pollAndDispatch pulls one job and submits it to the worker pool.
func (a *agent) pollAndDispatch(ctx context.Context) {
	if a.pool.ActiveWorkers() >= a.config.maxConcurrency {
		return
	}

	jobID, _, err := a.pullJob(ctx)
	if err != nil {
		if !errors.Is(err, ErrNoJobAvailable) {
			a.logger.Error("pull job failed", "error", err)
		}
		return
	}
	if jobID == "" {
		return
	}

	err = a.pool.Submit(&agentPoolJob{
		a:     a,
		ctx:   a.drainCtx,
		jobID: jobID,
	})
	if err != nil {
		a.logger.Error("submit job to pool failed", "job_id", jobID, "error", err)
		a.reportJobFailed(ctx, jobID, "worker pool full")
	}
}

// --- pullJob ---

// pullJob calls GET /api/agent/job/poll.
// 401 retry is handled transparently by the transport layer.
// Returns (jobID, scanner, error). Returns ErrNoJobAvailable on 204.
func (a *agent) pullJob(ctx context.Context) (string, string, error) {
	jobID, scanner, err := a.client.DoPollJob(ctx)
	if err != nil {
		return "", "", err
	}
	if jobID == "" {
		return "", "", ErrNoJobAvailable
	}
	return jobID, scanner, nil
}

// --- Job execution ---

// executeJob fetches job detail, runs the scanner, and reports the result.
func (a *agent) executeJob(ctx context.Context, jobID string) error {
	a.logger.Info("executing job", "job_id", jobID)

	// 1. Get job detail
	detail, err := a.getJobDetail(ctx, jobID)
	if err != nil {
		a.reportJobFailed(ctx, jobID, fmt.Sprintf("get detail: %v", err))
		return err
	}

	// 2. Build Job object
	j := newJob(detail)
	j.(*job).artifactDownloadFn = a.client.GetArtifactPresignedURL

	// 3. Attach job logger + log transport
	scannerName := a.scannerName
	if detail.Scanner != nil {
		scannerName = strings.ToLower(*detail.Scanner)
	}
	jobLogger, bufHandler := newJobLogger(jobID, scannerName, a.config.logger)
	j.(*job).logger = jobLogger

	logCtx, cancelLog := context.WithCancel(ctx)
	logSender := &agentLogSender{client: a.client}
	lt := newLogTransport(jobID, bufHandler, logSender, a.config.logger)
	var logWg sync.WaitGroup
	logWg.Add(1)
	go func() {
		defer logWg.Done()
		lt.Run(logCtx)
	}()

	// 4. Report job started
	jobLogger.Info("job started", "scanner", scannerName)
	a.reportJobStarted(ctx, jobID)

	// 5. Prepare repository if needed
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
			cancelLog()
			logWg.Wait()
			return err
		}
		defer j.(*job).cleanupRepository()
	}

	// 6. Start job heartbeat
	hbCtx, cancelHB := context.WithCancel(ctx)
	go a.jobHeartbeatLoop(hbCtx, jobID)

	// 7. Execute scanner
	scanErr := a.scanner.Scan(ctx, j, func(res Result) {
		a.importResult(ctx, jobID, res)
	})

	// 8. Report final status
	if scanErr != nil {
		jobLogger.Error("job failed", "error", scanErr.Error())
	} else {
		jobLogger.Info("job completed")
	}

	cancelLog()
	logWg.Wait()
	cancelHB()

	if scanErr != nil {
		a.reportJobFailed(ctx, jobID, scanErr.Error())
		return scanErr
	}
	a.reportJobCompleted(ctx, jobID)
	return nil
}

func (a *agent) getJobDetail(ctx context.Context, jobID string) (*api.JobDetail, error) {
	var detail *api.JobDetail
	err := a.retrier.Do(ctx, func() error {
		res, err := a.client.GetJobDetailWithResponse(ctx, jobID)
		if err != nil {
			return err
		}
		if res.StatusCode() >= 400 {
			return &APIError{StatusCode: res.StatusCode(), Response: string(res.Body)}
		}
		detail = res.JSON200
		return nil
	})
	return detail, err
}

// --- Job lifecycle API calls ---

func (a *agent) reportJobStarted(ctx context.Context, jobID string) {
	body := api.JobStartRequest{JobId: &jobID}
	_, _ = a.client.JobStartWithResponse(ctx, body)
}

func (a *agent) reportJobCompleted(ctx context.Context, jobID string) {
	body := api.JobCompletedRequest{JobId: &jobID}
	_, _ = a.client.JobCompletedWithResponse(ctx, body)
}

func (a *agent) reportJobFailed(ctx context.Context, jobID string, description string) {
	body := api.JobFailedRequest{JobId: &jobID, Description: &description}
	_, _ = a.client.JobFailedWithResponse(ctx, body)
}

func (a *agent) jobHeartbeatLoop(ctx context.Context, jobID string) {
	ticker := time.NewTicker(jobHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			body := api.JobHeartbeatRequest{JobId: &jobID}
			_, err := a.client.JobHeartbeatWithResponse(ctx, body)
			if err != nil {
				a.logger.Warn("job heartbeat failed", "job_id", jobID, "error", err)
			}
		}
	}
}

// --- Import results ---

func (a *agent) importResult(ctx context.Context, jobID string, res Result) {
	if domains := res.GetDomains(); len(domains) > 0 {
		a.importDomains(ctx, jobID, domains)
	}
	if services := res.GetServices(); len(services) > 0 {
		a.importServices(ctx, jobID, services)
	}
	if findings := res.GetWebFindings(); len(findings) > 0 {
		a.importWebFindings(ctx, jobID, findings)
	}
	if findings := res.GetSASTFindings(); len(findings) > 0 {
		a.importSASTFindings(ctx, jobID, findings)
	}
}

func (a *agent) importDomains(ctx context.Context, jobID string, domains []Domain) {
	apiDomains := toAPIDomains(domains)
	for i := 0; i < len(apiDomains); i += agentMaxBatchSize {
		end := min(i+agentMaxBatchSize, len(apiDomains))
		chunk := apiDomains[i:end]
		err := a.retrier.Do(ctx, func() error {
			body := api.PushAssetsJSONRequestBody{Domains: &chunk, JobId: &jobID}
			res, err := a.client.PushAssetsWithResponse(ctx, body)
			if err != nil {
				return err
			}
			if res.StatusCode() >= 400 {
				return &APIError{StatusCode: res.StatusCode(), Response: string(res.Body)}
			}
			return nil
		})
		if err != nil {
			a.logger.Error("import domains failed", "job_id", jobID, "error", err)
		}
	}
}

func (a *agent) importServices(ctx context.Context, jobID string, services []Service) {
	apiServices := toAPIServices(services)
	for i := 0; i < len(apiServices); i += agentMaxBatchSize {
		end := min(i+agentMaxBatchSize, len(apiServices))
		chunk := apiServices[i:end]
		err := a.retrier.Do(ctx, func() error {
			body := api.PushAssetsJSONRequestBody{Services: &chunk, JobId: &jobID}
			res, err := a.client.PushAssetsWithResponse(ctx, body)
			if err != nil {
				return err
			}
			if res.StatusCode() >= 400 {
				return &APIError{StatusCode: res.StatusCode(), Response: string(res.Body)}
			}
			return nil
		})
		if err != nil {
			a.logger.Error("import services failed", "job_id", jobID, "error", err)
		}
	}
}

func (a *agent) importWebFindings(ctx context.Context, jobID string, findings []WebFinding) {
	apiFindings := toAPIWebFindings(findings)
	err := a.retrier.Do(ctx, func() error {
		body := api.PushFindingsJSONRequestBody{WebFindings: &apiFindings, JobId: &jobID}
		res, err := a.client.PushFindingsWithResponse(ctx, body)
		if err != nil {
			return err
		}
		if res.StatusCode() >= 400 {
			return &APIError{StatusCode: res.StatusCode(), Response: string(res.Body)}
		}
		return nil
	})
	if err != nil {
		a.logger.Error("import web findings failed", "job_id", jobID, "error", err)
	}
}

func (a *agent) importSASTFindings(ctx context.Context, jobID string, findings []SASTFinding) {
	apiFindings := toAPISASTFindings(findings)
	err := a.retrier.Do(ctx, func() error {
		body := api.PushFindingsJSONRequestBody{CodeFindings: &apiFindings, JobId: &jobID}
		res, err := a.client.PushFindingsWithResponse(ctx, body)
		if err != nil {
			return err
		}
		if res.StatusCode() >= 400 {
			return &APIError{StatusCode: res.StatusCode(), Response: string(res.Body)}
		}
		return nil
	})
	if err != nil {
		a.logger.Error("import SAST findings failed", "job_id", jobID, "error", err)
	}
}

// --- CI mode ---

// detectGitContext returns CI context, applying repoDir override if configured.
func (a *agent) detectGitContext() *CIContext {
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

func (a *agent) executeCIJob(ctx context.Context, ci *CIContext) error {
	// Resolve params from env vars
	params := resolveParamsFromEnv(a.scanner.Params(), ci.Parameters)

	// Create job via API
	apiReq := ciContextToAPIRequest(ci, a.scannerName)
	if len(params) > 0 {
		apiReq.Parameters = &params
	}

	res, err := a.client.CreateJobWithResponse(ctx, apiReq)
	if err != nil {
		return fmt.Errorf("create CI job: %w", err)
	}
	if res.StatusCode() >= 400 {
		return &APIError{StatusCode: res.StatusCode(), Response: string(res.Body)}
	}
	if res.JSON200 == nil || res.JSON200.JobId == nil {
		return fmt.Errorf("create CI job: no job ID returned")
	}
	if res.JSON200.Success != nil && !*res.JSON200.Success {
		msg := ""
		if res.JSON200.Message != nil {
			msg = *res.JSON200.Message
		}
		return fmt.Errorf("create CI job: %s", msg)
	}

	jobID := *res.JSON200.JobId
	a.logger.Info("CI job created", "job_id", jobID)

	// Build job from CIContext
	j := newCIJob(jobID, ci, a.scannerName, params)

	// Attach job logger + log transport
	ciJobLogger, ciBufHandler := newJobLogger(jobID, a.scannerName, a.config.logger)
	j.(*job).logger = ciJobLogger

	ciLogCtx, cancelCILog := context.WithCancel(ctx)
	ciLogSender := &agentLogSender{client: a.client}
	ciLt := newLogTransport(jobID, ciBufHandler, ciLogSender, a.config.logger)
	var ciLogWg sync.WaitGroup
	ciLogWg.Add(1)
	go func() {
		defer ciLogWg.Done()
		ciLt.Run(ciLogCtx)
	}()

	ciJobLogger.Info("job started", "ci_provider", string(ci.Provider))
	a.reportJobStarted(ctx, jobID)

	if err := j.(*job).prepareRepository(ctx); err != nil {
		a.reportJobFailed(ctx, jobID, fmt.Sprintf("prepare repo: %v", err))
		cancelCILog()
		ciLogWg.Wait()
		return err
	}
	defer j.(*job).cleanupRepository()

	hbCtx, cancelHB := context.WithCancel(ctx)
	go a.jobHeartbeatLoop(hbCtx, jobID)

	scanErr := a.scanner.Scan(ctx, j, func(res Result) {
		a.importResult(ctx, jobID, res)
	})

	if scanErr != nil {
		ciJobLogger.Error("job failed", "error", scanErr.Error())
	} else {
		ciJobLogger.Info("job completed")
	}

	cancelCILog()
	ciLogWg.Wait()
	cancelHB()

	if scanErr != nil {
		a.reportJobFailed(ctx, jobID, scanErr.Error())
		return scanErr
	}
	a.reportJobCompleted(ctx, jobID)
	return nil
}

// --- Scanner metadata update ---

// syncScannerMetadata calls PATCH /api/agent/scanner to sync scanner config
// using the scanner's own metadata (name, asset types, params, etc.).
func (a *agent) syncScannerMetadata(ctx context.Context) {
	name := a.scanner.Name()
	req := api.UpdateAgentScannerRequest{Name: &name}
	var needsUpdate bool

	if dn, ok := a.scanner.(interface{ DisplayName() string }); ok {
		if displayName := dn.DisplayName(); displayName != "" {
			req.DisplayName = &displayName
			needsUpdate = true
		}
	}

	if ps := a.scanner.Params(); len(ps) > 0 {
		schema := ParamsToJSONSchema(ps)
		if schema != nil {
			req.ParamsSchema = &schema
			needsUpdate = true
		}
	}

	if types := a.scanner.AssetTypes(); len(types) > 0 {
		assetTypes := make([]api.AssetTypes, len(types))
		for i, t := range types {
			assetTypes[i] = api.AssetTypes(t)
		}
		req.AssetTypes = &assetTypes
		needsUpdate = true
	}

	if rc, ok := a.scanner.(interface{ SupportsRetest() bool }); ok {
		if supportsRetest := rc.SupportsRetest(); supportsRetest {
			req.SupportsRetest = &supportsRetest
			needsUpdate = true
		}
	}

	if !needsUpdate {
		return
	}
	if err := a.client.UpdateScanner(ctx, req); err != nil {
		a.logger.Warn("failed to sync scanner metadata", "error", err)
	} else {
		a.logger.Info("scanner metadata synced")
	}
}

// --- Worker pool job wrapper ---

type agentPoolJob struct {
	a     *agent
	ctx   context.Context // drainCtx
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

