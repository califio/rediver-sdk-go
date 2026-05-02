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

	agentv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/agent/v1"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/califio/rediver-sdk-go/internal/auth"
	"github.com/califio/rediver-sdk-go/internal/transport"
	"github.com/califio/rediver-sdk-go/internal/worker"
	"github.com/califio/rediver-sdk-go/utils"
)

const (
	agentMaxBatchSize = 500
)

// agent is the internal per-scanner agent (unexported).
type agent struct {
	scanner     Scanner
	scannerName string        // scanner name in DB (normalized)
	agentID     string        // from generate-token response
	config      *runnerConfig // shared from Runner (read-only after creation)
	token       atomic.Value  // stores string — current agent token

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

	mu sync.Mutex // guards token refresh
}

// newAgent creates a fully initialized agent by generating a token for the given scanner.
// persistent=true for Worker/Dispatcher modes, false for Task/CI modes.
func newAgent(ctx context.Context, s Scanner, clusterToken, serverURL string, persistent, syncMetadata bool, config *runnerConfig) (*agent, error) {
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

// --- Run modes ---

// run starts the Worker mode lifecycle: heartbeat + poll loops.
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
	return nil
}

// runOnce runs Task mode: poll one job, execute it, revoke token, return.
func (a *agent) runOnce(ctx context.Context) error {
	var jobID string
	if a.config.directJobID != "" {
		jobID = a.config.directJobID
		a.logger.Info("direct job execution", "job_id", jobID)
	} else {
		var err error
		jobID, _, err = a.pullJob(ctx)
		if errors.Is(err, ErrNoJobAvailable) {
			a.logger.Info("no job available, exiting")
			_ = a.tokenManager.RevokeToken(ctx)
			return nil
		}
		if err != nil {
			_ = a.tokenManager.RevokeToken(ctx)
			return err
		}
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
func (a *agent) runDispatcher(ctx context.Context, handler JobHandler) error {
	a.drainCtx, a.cancelDrain = context.WithCancel(context.Background())
	defer a.cancelDrain()

	a.logger.Info("agent started (dispatcher mode)")

	go a.heartbeatLoop(a.drainCtx)

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

// --- Job execution ---

func (a *agent) executeJob(ctx context.Context, jobID string) error {
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
			cancelLog()
			logWg.Wait()
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

func (a *agent) getJobDetail(ctx context.Context, jobID string) (*agentv1.GetJobDetailResponse, error) {
	var detail *agentv1.GetJobDetailResponse
	err := a.retrier.Do(ctx, func() error {
		var err error
		detail, err = a.client.GetJobDetail(ctx, jobID)
		return err
	})
	return detail, err
}

// --- Job lifecycle API calls ---

func (a *agent) reportJobStarted(ctx context.Context, jobID string) {
	_ = a.client.JobStart(ctx, jobID)
}

func (a *agent) reportJobCompleted(ctx context.Context, jobID string) {
	_ = a.client.JobCompleted(ctx, jobID)
}

func (a *agent) reportJobFailed(ctx context.Context, jobID string, description string) {
	_ = a.client.JobFailed(ctx, jobID, description)
}

// --- Import results ---

func (a *agent) importResult(ctx context.Context, jobID string, res Result, headSHA string) {
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
		if headSHA != "" {
			for i := range findings {
				if findings[i].CommitSha == "" {
					findings[i].CommitSha = headSHA
				}
			}
		}
		a.importSASTFindings(ctx, jobID, findings)
	}
}

func (a *agent) importDomains(ctx context.Context, jobID string, domains []Domain) {
	apiDomains := toProtoDomains(domains)
	for i := 0; i < len(apiDomains); i += agentMaxBatchSize {
		end := min(i+agentMaxBatchSize, len(apiDomains))
		chunk := apiDomains[i:end]
		err := a.retrier.Do(ctx, func() error {
			return a.client.PushAssets(ctx, &agentv1.PushAssetsRequest{
				JobId:   jobID,
				Domains: chunk,
			})
		})
		if err != nil {
			a.logger.Error("import domains failed", "job_id", jobID, "error", err)
		}
	}
}

func (a *agent) importServices(ctx context.Context, jobID string, services []Service) {
	apiServices := toProtoServices(services)
	for i := 0; i < len(apiServices); i += agentMaxBatchSize {
		end := min(i+agentMaxBatchSize, len(apiServices))
		chunk := apiServices[i:end]
		err := a.retrier.Do(ctx, func() error {
			return a.client.PushAssets(ctx, &agentv1.PushAssetsRequest{
				JobId:    jobID,
				Services: chunk,
			})
		})
		if err != nil {
			a.logger.Error("import services failed", "job_id", jobID, "error", err)
		}
	}
}

func (a *agent) importWebFindings(ctx context.Context, jobID string, findings []WebFinding) {
	err := a.retrier.Do(ctx, func() error {
		return a.client.PushFindings(ctx, &agentv1.PushFindingsRequest{
			JobId:       jobID,
			WebFindings: toProtoWebFindings(findings),
		})
	})
	if err != nil {
		a.logger.Error("import web findings failed", "job_id", jobID, "error", err)
	}
}

func (a *agent) importSASTFindings(ctx context.Context, jobID string, findings []SASTFinding) {
	err := a.retrier.Do(ctx, func() error {
		return a.client.PushFindings(ctx, &agentv1.PushFindingsRequest{
			JobId:        jobID,
			CodeFindings: toProtoSASTFindings(findings),
		})
	})
	if err != nil {
		a.logger.Error("import SAST findings failed", "job_id", jobID, "error", err)
	}
}

// --- CI mode ---

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

	j := newCIJob(jobID, ci, a.scannerName, params)

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
		a.importResult(ctx, jobID, res, "")
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

func (a *agent) syncScannerMetadata(ctx context.Context) {
	name := a.scanner.Name()
	req := &agentv1.UpdateScannerRequest{Name: name}
	var needsUpdate bool

	if dn, ok := a.scanner.(interface{ DisplayName() string }); ok {
		if displayName := dn.DisplayName(); displayName != "" {
			req.DisplayName = &displayName
			needsUpdate = true
		}
	}

	if ps, ok := a.scanner.(interface{ ParamsSchema() map[string]interface{} }); ok {
		if schema := ps.ParamsSchema(); schema != nil {
			s, err := structpb.NewStruct(schema)
			if err == nil {
				req.ParamsSchema = s
				needsUpdate = true
			}
		}
	} else if ps := a.scanner.Params(); len(ps) > 0 {
		schema := ParamsToJSONSchema(ps)
		if schema != nil {
			s, err := structpb.NewStruct(schema)
			if err == nil {
				req.ParamsSchema = s
				needsUpdate = true
			}
		}
	}

	if types := a.scanner.AssetTypes(); len(types) > 0 {
		assetTypes := make([]agentv1.AssetType, len(types))
		for i, t := range types {
			assetTypes[i] = toProtoAssetType(t)
		}
		req.AssetTypes = assetTypes
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
