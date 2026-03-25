package rediver

import (
	"context"
	"errors"
	"fmt"
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
	"golang.org/x/sync/errgroup"
)

const (
	agentHeartbeatInterval = 60 * time.Second
	jobHeartbeatInterval   = 60 * time.Second
	agentMaxBatchSize      = 500
)

// Agent manages the scanner lifecycle with 2-token authentication.
type Agent struct {
	serverURL    string
	clusterToken string
	config       *agentConfig
	tokenManager *auth.TokenManager
	client       *transport.Client
	scanners     map[string]Scanner // registry: lowercase name -> scanner
	pool         *worker.Pool
	retrier      *retrier
	running      atomic.Bool // set in Run(), checked in Register()
	drainCtx     context.Context // stays alive during graceful shutdown for running jobs
}

// NewAgent creates a new agent instance.
func NewAgent(serverURL, clusterToken string, opts ...Option) (*Agent, error) {
	if serverURL == "" {
		if env := resolveServerURL(""); env != "" {
			serverURL = env
		} else {
			return nil, fmt.Errorf("%w: server URL is required", ErrInvalidConfig)
		}
	}
	if clusterToken == "" {
		if env := resolveClusterToken(""); env != "" {
			clusterToken = env
		} else {
			return nil, fmt.Errorf("%w: cluster token is required", ErrInvalidConfig)
		}
	}

	config := defaultAgentConfig()
	for _, opt := range opts {
		opt(config)
	}

	// Create Persister (nil for task mode)
	var persist *auth.Persister
	if config.runMode == RunModeWorker && config.agentIDPath != "" {
		persist = auth.NewPersister(config.agentIDPath)
	}

	// Create TokenManager
	tm := auth.NewTokenManager(clusterToken, config.runMode, persist)

	// Force agent ID if provided
	if config.agentID != "" {
		// Will be sent in registration request
	}

	// Create transport client (wires registerFn into TokenManager)
	client, err := transport.NewClient(serverURL, tm, config.httpClient)
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}

	return &Agent{
		serverURL:    serverURL,
		clusterToken: clusterToken,
		config:       config,
		tokenManager: tm,
		client:       client,
		scanners:     make(map[string]Scanner),
		retrier:      newRetrier(config.retryPolicy),
	}, nil
}

// Register adds scanners to the agent's internal registry.
// Scanner names are normalized to lowercase. Must be called before Run().
func (a *Agent) Register(scanners ...Scanner) error {
	if a.running.Load() {
		return fmt.Errorf("%w: cannot register scanners after Run() has been called", ErrInvalidConfig)
	}
	for _, s := range scanners {
		name := strings.ToLower(s.Name())
		if name == "" {
			return fmt.Errorf("%w: scanner name cannot be empty", ErrInvalidConfig)
		}
		if _, exists := a.scanners[name]; exists {
			return fmt.Errorf("%w: duplicate scanner name: %q", ErrInvalidConfig, name)
		}
		a.scanners[name] = s
	}
	return nil
}

// Run starts the agent lifecycle based on the configured run mode.
func (a *Agent) Run(ctx context.Context, runOpts ...RunOption) error {
	if !a.running.CompareAndSwap(false, true) {
		return fmt.Errorf("%w: agent already running", ErrInvalidConfig)
	}
	defer a.running.Store(false)

	cfg := &runConfig{}
	for _, opt := range runOpts {
		opt(cfg)
	}

	if len(a.scanners) == 0 {
		return fmt.Errorf("%w: at least one scanner must be registered", ErrInvalidConfig)
	}

	// CI mode uses lightweight connect (no scanners in request)
	if a.config.runMode == RunModeCI {
		return a.RunAsCI(ctx)
	}

	// Task mode uses lightweight connect with scanners
	if a.config.runMode == RunModeTask {
		connectReq := auth.ConnectRequest{
			Scanners: a.scannerNamesList(),
		}
		var connResp *auth.ConnectResponse
		if err := a.retrier.Do(ctx, func() error {
			var connErr error
			connResp, connErr = a.tokenManager.Connect(ctx, connectReq)
			return connErr
		}); err != nil {
			return fmt.Errorf("connect: %w", err)
		}

		// Remap + update scanner metadata if scanners were returned
		if connResp != nil && len(connResp.Scanners) > 0 {
			a.remapScannerNames(connResp.Scanners)
			a.updateScannerMetadata(ctx, connResp.Scanners, connResp.System)
		}

		a.config.logger.Info("connected to server (task mode)",
			"agent_id", a.tokenManager.AgentID(),
			"scanners", a.scannerNamesList(),
		)

		a.pool = worker.NewPool(a.config.maxConcurrency, a.config.maxConcurrency*2)
		return a.runTask(ctx, cfg)
	}

	// Worker/Daemon mode: per-scanner agents via generate-token
	return a.runWithScannerAgents(ctx)
}

// RunAsWorker explicitly runs in daemon mode (long-running poll loop).
func (a *Agent) RunAsWorker(ctx context.Context) error {
	a.config.runMode = RunModeWorker
	return a.Run(ctx)
}

// RunAsTask explicitly runs in task mode (pull 1 job, execute, exit).
func (a *Agent) RunAsTask(ctx context.Context, opts ...RunOption) error {
	a.config.runMode = RunModeTask
	return a.Run(ctx, opts...)
}

// RunAsCI explicitly runs in CI mode (detect env, create job, scan local repo, exit).
// CI mode uses lightweight token exchange (POST /api/agent/token) instead of full registration.
func (a *Agent) RunAsCI(ctx context.Context) error {
	a.config.runMode = RunModeCI
	// Try to set running; if already set (called from Run()), skip the guard.
	ownedRunning := a.running.CompareAndSwap(false, true)
	if ownedRunning {
		defer a.running.Store(false)
	}

	if len(a.scanners) == 0 {
		return fmt.Errorf("%w: at least one scanner must be registered", ErrInvalidConfig)
	}

	// Lightweight connect: exchange cluster token for agent token
	connectReq := auth.ConnectRequest{
		Scanners: a.scannerNamesList(),
	}
	var connResp *auth.ConnectResponse
	if err := a.retrier.Do(ctx, func() error {
		var connErr error
		connResp, connErr = a.tokenManager.Connect(ctx, connectReq)
		return connErr
	}); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	// Remap scanner names if backend resolved differently (e.g., custom_ prefix)
	if connResp != nil && len(connResp.Scanners) > 0 {
		a.remapScannerNames(connResp.Scanners)
	}

	a.config.logger.Info("connected to server (CI mode)",
		"agent_id", a.tokenManager.AgentID(),
		"scanners", a.scannerNamesList(),
	)

	// Create worker pool
	a.pool = worker.NewPool(a.config.maxConcurrency, a.config.maxConcurrency*2)

	return a.runCI(ctx)
}

// scannerNamesList returns scanner names for logging.
func (a *Agent) scannerNamesList() []string {
	names := make([]string, 0, len(a.scanners))
	for name := range a.scanners {
		names = append(names, name)
	}
	return names
}

// remapScannerNames updates the internal scanner registry using backend-resolved names.
// For example, if a tenant agent registers "semgrep" but backend creates "custom_semgrep",
// the registry key is updated so job dispatch works correctly.
func (a *Agent) remapScannerNames(scanners []auth.RegisteredScannerInfo) {
	for _, info := range scanners {
		if info.RequestName != info.Name {
			if s, ok := a.scanners[info.RequestName]; ok {
				delete(a.scanners, info.RequestName)
				a.scanners[info.Name] = s
				a.config.logger.Info("scanner name remapped",
					"from", info.RequestName,
					"to", info.Name,
					"system", info.System,
				)
			}
		}
	}
}

func (a *Agent) buildRegistrationRequest() auth.RegistrationRequest {
	return auth.RegistrationRequest{
		Scanners:   a.scannerNamesList(),
		AgentID:    a.config.agentID,
		Hostname:   a.config.hostname,
		IPAddress:  utils.GetIPAddress(),
		Version:    a.config.version,
		SdkVersion: SdkVersion,
	}
}

// updateScannerMetadata calls PATCH /api/agent/scanner for each scanner the agent "owns".
// System agents update system scanners; tenant agents update tenant scanners.
func (a *Agent) updateScannerMetadata(ctx context.Context, registered []auth.RegisteredScannerInfo, isSystem bool) {
	for _, info := range registered {
		s, ok := a.scanners[info.Name]
		if !ok {
			continue
		}

		// Only update scanners we "own"
		if info.System != isSystem {
			continue
		}

		update := a.buildScannerUpdate(s, info)
		if update == nil {
			continue
		}

		if err := a.client.UpdateScanner(ctx, *update); err != nil {
			a.config.logger.Warn("failed to update scanner metadata",
				"scanner", info.Name,
				"error", err,
			)
		} else {
			a.config.logger.Info("scanner metadata updated",
				"scanner", info.Name,
			)
		}
	}
}

// buildScannerUpdate compares local scanner metadata with backend state and returns
// an update request if they differ. Returns nil if no update is needed.
func (a *Agent) buildScannerUpdate(s Scanner, info auth.RegisteredScannerInfo) *api.UpdateAgentScannerRequest {
	var needsUpdate bool
	req := api.UpdateAgentScannerRequest{
		Name: &info.Name,
	}

	// Check display name
	var displayName string
	if dn, ok := s.(interface{ DisplayName() string }); ok {
		displayName = dn.DisplayName()
	}
	if displayName != "" && displayName != info.DisplayName {
		req.DisplayName = &displayName
		needsUpdate = true
	}

	// Check params schema
	if ps := s.Params(); len(ps) > 0 {
		schema := ParamsToJSONSchema(ps)
		if schema != nil {
			req.ParamsSchema = &schema
			needsUpdate = true
		}
	}

	// Check asset types
	if types := s.AssetTypes(); len(types) > 0 {
		assetTypes := make([]api.AssetTypes, len(types))
		for i, t := range types {
			assetTypes[i] = api.AssetTypes(t)
		}
		req.AssetTypes = &assetTypes
		needsUpdate = true
	}

	// Check retest support
	if rc, ok := s.(interface{ SupportsRetest() bool }); ok {
		supportsRetest := rc.SupportsRetest()
		if supportsRetest {
			req.SupportsRetest = &supportsRetest
			needsUpdate = true
		}
	}

	if !needsUpdate {
		return nil
	}
	return &req
}

// registerAndInitPool performs Worker mode registration and creates the worker pool.
// Used by both Run() (daemon mode) and ListenForJobs().
func (a *Agent) registerAndInitPool(ctx context.Context) error {
	regReq := a.buildRegistrationRequest()

	var regResp *auth.RegistrationResponse
	if err := a.retrier.Do(ctx, func() error {
		var regErr error
		regResp, regErr = a.tokenManager.Register(ctx, regReq)
		return regErr
	}); err != nil {
		return fmt.Errorf("register: %w", err)
	}

	if regResp != nil && len(regResp.Scanners) > 0 {
		a.remapScannerNames(regResp.Scanners)
		a.updateScannerMetadata(ctx, regResp.Scanners, regResp.System)
	}

	a.config.logger.Info("registered with server",
		"agent_id", a.tokenManager.AgentID(),
		"scanners", a.scannerNamesList(),
		"mode", a.config.runMode.String(),
	)

	a.pool = worker.NewPool(a.config.maxConcurrency, a.config.maxConcurrency*2)
	return nil
}

// --- Per-scanner agent mode (Worker) ---

// runWithScannerAgents creates independent scannerAgents via generate-token and runs them.
func (a *Agent) runWithScannerAgents(ctx context.Context) error {
	persistent := a.config.runMode == RunModeWorker
	hostname := a.config.hostname
	if hostname == "" {
		hostname = utils.GetIPAddress()
	}
	if hostname == "" && persistent {
		return fmt.Errorf("%w: hostname or IP required for worker mode", ErrInvalidConfig)
	}

	// Phase 1: Generate tokens for all scanners
	var scannerAgents []*scannerAgent
	for name, s := range a.scanners {
		sa, err := a.createScannerAgent(ctx, name, s, persistent, hostname)
		if err != nil {
			// First start: ANY failure → shutdown entire process
			return fmt.Errorf("failed to initialize scanner %s: %w", name, err)
		}
		scannerAgents = append(scannerAgents, sa)
	}

	a.config.logger.Info("all scanner-agents initialized",
		"count", len(scannerAgents),
		"mode", a.config.runMode.String(),
	)

	// Phase 2: Launch all scanner-agents via errgroup
	g, gCtx := errgroup.WithContext(ctx)
	for _, sa := range scannerAgents {
		g.Go(func() error { return sa.run(gCtx) })
	}
	return g.Wait()
}

// createScannerAgent calls POST /api/agent/generate-token for a single scanner
// and returns a fully initialized scannerAgent.
func (a *Agent) createScannerAgent(
	ctx context.Context, name string, s Scanner,
	persistent bool, hostname string,
) (*scannerAgent, error) {
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
		return nil, err
	}

	// Remap scanner name if backend resolved differently
	scannerInfo := resp.Scanner
	if scannerInfo.RequestName != "" && scannerInfo.RequestName != scannerInfo.Name {
		a.config.logger.Info("scanner name remapped",
			"from", scannerInfo.RequestName,
			"to", scannerInfo.Name,
		)
	}

	// Update scanner metadata
	a.updateScannerMetadata(ctx, []auth.RegisteredScannerInfo{scannerInfo}, scannerInfo.System)

	sa, err := newScannerAgent(a, s, resp, genReq, a.config, a.client)
	if err != nil {
		return nil, err
	}

	a.config.logger.Info("scanner-agent created",
		"scanner", scannerInfo.Name,
		"agent_id", resp.AgentID,
		"persistent", persistent,
	)

	return sa, nil
}

// --- Daemon mode (legacy — used by registerAndInitPool path) ---

func (a *Agent) runDaemon(ctx context.Context) error {
	// drainCtx stays alive during graceful shutdown so running jobs
	// can continue sending heartbeats and reporting results.
	// Only cancelled after all jobs complete or shutdown timeout.
	drainCtx, cancelDrain := context.WithCancel(context.Background())
	defer cancelDrain()

	// Agent heartbeat uses drainCtx — continues during drain
	go a.agentHeartbeatLoop(drainCtx)

	// Poll loop stops on ctx (SIGTERM)
	a.drainCtx = drainCtx
	go a.pollLoop(ctx)

	// Wait for shutdown signal
	<-ctx.Done()
	a.config.logger.Info("shutting down...")

	// Graceful: wait for running jobs to complete
	done := make(chan struct{})
	go func() {
		a.pool.Shutdown()
		close(done)
	}()

	if a.config.shutdownTimeout > 0 {
		select {
		case <-done:
			a.config.logger.Info("all jobs completed")
		case <-time.After(a.config.shutdownTimeout):
			a.config.logger.Warn("shutdown timeout, forcing exit")
			// Cancel drainCtx first so running jobs see context cancellation,
			// then ShutdownNow waits for them to return.
			cancelDrain()
			a.pool.ShutdownNow()
		}
	} else {
		// Wait forever (default)
		<-done
		a.config.logger.Info("all jobs completed")
	}

	// All jobs finished (or forced) — stop agent heartbeat
	cancelDrain()
	return nil
}

// --- Task mode ---

func (a *Agent) runTask(ctx context.Context, cfg *runConfig) error {
	var jobID string
	var err error

	if cfg.jobID != "" {
		// Direct job ID: skip pull, go straight to execution
		jobID = cfg.jobID
	} else {
		// Pull once — if no job available, revoke token and exit
		jobID, err = a.pullJob(ctx)
		if errors.Is(err, ErrNoJobAvailable) {
			a.config.logger.Info("no job available, exiting")
			_ = a.tokenManager.RevokeToken(ctx)
			return nil
		}
		if err != nil {
			_ = a.tokenManager.RevokeToken(ctx)
			return err
		}
	}

	execErr := a.executeJob(ctx, jobID)

	// Always revoke token in task mode
	_ = a.tokenManager.RevokeToken(ctx)
	return execErr
}

// --- Poll loop (daemon) ---

func (a *Agent) pollLoop(ctx context.Context) {
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

func (a *Agent) pollAndDispatch(ctx context.Context) {
	if a.pool.ActiveWorkers() >= a.config.maxConcurrency {
		return // at capacity
	}

	jobID, err := a.pullJob(ctx)
	if err != nil {
		if !errors.Is(err, ErrNoJobAvailable) {
			a.config.logger.Error("pull job failed", "error", err)
		}
		return
	}

	// Submit to worker pool with drainCtx so running jobs
	// keep heartbeating during graceful shutdown.
	err = a.pool.Submit(&agentJob{
		agent: a,
		ctx:   a.drainCtx,
		jobID: jobID,
	})
	if err != nil {
		a.config.logger.Error("submit job failed", "job_id", jobID, "error", err)
		a.reportJobFailed(ctx, jobID, "worker pool full")
	}
}

// --- Job pull ---

func (a *Agent) pullJob(ctx context.Context) (string, error) {
	res, err := a.client.RequestJobWithResponse(ctx)
	if err != nil {
		return "", err
	}

	// 401 → re-register with cluster token and retry once
	if res.StatusCode() == 401 {
		oldToken := a.tokenManager.AgentToken()
		if rerr := a.tokenManager.Reregister(ctx, oldToken); rerr != nil {
			return "", fmt.Errorf("re-registration after 401: %w", rerr)
		}
		a.config.logger.Info("re-registered after token expiry")

		// Retry pull with new token
		res, err = a.client.RequestJobWithResponse(ctx)
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

// --- Job execution ---

func (a *Agent) executeJob(ctx context.Context, jobID string) error {
	a.config.logger.Info("executing job", "job_id", jobID)

	// 1. Get job detail
	detail, err := a.getJobDetail(ctx, jobID)
	if err != nil {
		a.reportJobFailed(ctx, jobID, fmt.Sprintf("get detail: %v", err))
		return err
	}

	// 2. Lookup scanner from registry (lowercase normalized)
	scannerName := ""
	if detail.Scanner != nil {
		scannerName = strings.ToLower(*detail.Scanner)
	}
	handler, ok := a.scanners[scannerName]
	if !ok {
		msg := fmt.Sprintf("no handler for scanner %q", scannerName)
		a.reportJobFailed(ctx, jobID, msg)
		return fmt.Errorf("%w: %s", ErrInvalidJob, msg)
	}

	// 3. Build Job object
	ci := a.tokenManager.GetClusterInfo()
	j := newJob(detail, ClusterInfo{
		ID:                 ci.ID,
		Name:               ci.Name,
		AgentType:          ci.AgentType,
		Tags:               ci.Tags,
		AcceptUntaggedJobs: ci.AcceptUntaggedJobs,
		MaxConcurrentJobs:  ci.MaxConcurrentJobs,
	})
	j.(*job).artifactDownloadFn = a.client.GetArtifactPresignedURL

	// 4. Attach job logger + log transport
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

	// 5. Report job started
	jobLogger.Info("job started", "scanner", scannerName)
	a.reportJobStarted(ctx, jobID)

	// 5b. Prepare repository if job has a repo target
	if _, hasRepo := j.Repository(); hasRepo {
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
	var scanErr error
	err = handler.Scan(ctx, j, func(res Result) {
		a.importResult(ctx, jobID, res)
	})
	if err != nil {
		scanErr = err
	}

	// 8. Report final status (log before stopping transport so entry gets flushed)
	if scanErr != nil {
		jobLogger.Error("job failed", "error", scanErr.Error())
	} else {
		jobLogger.Info("job completed")
	}

	// Stop log transport (triggers final flush)
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

func (a *Agent) getJobDetail(ctx context.Context, jobID string) (*api.JobDetail, error) {
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

func (a *Agent) reportJobStarted(ctx context.Context, jobID string) {
	body := api.JobStartRequest{JobId: &jobID}
	_, _ = a.client.JobStartWithResponse(ctx, body)
}

func (a *Agent) reportJobCompleted(ctx context.Context, jobID string) {
	body := api.JobCompletedRequest{JobId: &jobID}
	_, _ = a.client.JobCompletedWithResponse(ctx, body)
}

func (a *Agent) reportJobFailed(ctx context.Context, jobID string, description string) {
	body := api.JobFailedRequest{JobId: &jobID, Description: &description}
	_, _ = a.client.JobFailedWithResponse(ctx, body)
}

// --- Heartbeats ---

func (a *Agent) agentHeartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(agentHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.sendAgentHeartbeat(ctx)
		}
	}
}

func (a *Agent) sendAgentHeartbeat(ctx context.Context) {
	currentJobs := int32(0)
	if a.pool != nil {
		currentJobs = int32(a.pool.ActiveWorkers())
	}
	body := api.AgentHeartbeatRequest{
		CurrentJobs: &currentJobs,
	}
	_, err := a.client.AgentHeartbeatWithResponse(ctx, body)
	if err != nil {
		a.config.logger.Warn("agent heartbeat failed", "error", err)
	}
}

func (a *Agent) jobHeartbeatLoop(ctx context.Context, jobID string) {
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
				a.config.logger.Warn("job heartbeat failed", "job_id", jobID, "error", err)
			}
		}
	}
}

// --- Import results ---

func (a *Agent) importResult(ctx context.Context, jobID string, res Result) {
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

func (a *Agent) importDomains(ctx context.Context, jobID string, domains []Domain) {
	apiDomains := toAPIDomains(domains)

	for i := 0; i < len(apiDomains); i += agentMaxBatchSize {
		end := i + agentMaxBatchSize
		if end > len(apiDomains) {
			end = len(apiDomains)
		}
		chunk := apiDomains[i:end]

		err := a.retrier.Do(ctx, func() error {
			body := api.PushAssetsJSONRequestBody{
				Domains: &chunk,
				JobId:   &jobID,
			}
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
			a.config.logger.Error("import domains failed", "job_id", jobID, "error", err)
		} else {
			a.config.logger.Debug("imported domains", "job_id", jobID, "count", len(chunk))
		}
	}
}

func (a *Agent) importServices(ctx context.Context, jobID string, services []Service) {
	apiServices := toAPIServices(services)

	for i := 0; i < len(apiServices); i += agentMaxBatchSize {
		end := i + agentMaxBatchSize
		if end > len(apiServices) {
			end = len(apiServices)
		}
		chunk := apiServices[i:end]

		err := a.retrier.Do(ctx, func() error {
			body := api.PushAssetsJSONRequestBody{
				Services: &chunk,
				JobId:    &jobID,
			}
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
			a.config.logger.Error("import services failed", "job_id", jobID, "error", err)
		} else {
			a.config.logger.Debug("imported services", "job_id", jobID, "count", len(chunk))
		}
	}
}

func (a *Agent) importWebFindings(ctx context.Context, jobID string, findings []WebFinding) {
	apiFindings := toAPIWebFindings(findings)

	err := a.retrier.Do(ctx, func() error {
		body := api.PushFindingsJSONRequestBody{
			WebFindings: &apiFindings,
			JobId:       &jobID,
		}
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
		a.config.logger.Error("import web findings failed", "job_id", jobID, "error", err)
	}
}

func (a *Agent) importSASTFindings(ctx context.Context, jobID string, findings []SASTFinding) {
	apiFindings := toAPISASTFindings(findings)

	err := a.retrier.Do(ctx, func() error {
		body := api.PushFindingsJSONRequestBody{
			CodeFindings: &apiFindings,
			JobId:        &jobID,
		}
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
		a.config.logger.Error("import SAST findings failed", "job_id", jobID, "error", err)
	}
}

// --- Worker pool job wrapper ---

type agentJob struct {
	agent *Agent
	ctx   context.Context
	jobID string
}

func (j *agentJob) Execute(ctx context.Context) error {
	return j.agent.executeJob(j.ctx, j.jobID)
}

func (j *agentJob) OnEnqueue() {
	j.agent.config.logger.Debug("job enqueued", "job_id", j.jobID)
}

func (j *agentJob) OnError(err error) {
	j.agent.config.logger.Error("job failed", "job_id", j.jobID, "error", err)
}

func (j *agentJob) OnCompleted() {
	j.agent.config.logger.Debug("job completed", "job_id", j.jobID)
}

// --- Helpers ---

func resolveClusterToken(explicit string) string {
	if explicit != "" {
		return explicit
	}
	return strings.TrimSpace(os.Getenv("REDIVER_TOKEN"))
}

func resolveServerURL(explicit string) string {
	if explicit != "" {
		return explicit
	}
	return strings.TrimSpace(os.Getenv("REDIVER_URL"))
}

// --- CI mode ---

// detectGitContext returns CI context, applying repoDir override if configured.
func (a *Agent) detectGitContext() *CIContext {
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

func (a *Agent) runCI(ctx context.Context) error {
	ci := a.detectGitContext()
	if ci == nil {
		_ = a.tokenManager.RevokeToken(ctx)
		return fmt.Errorf("%w: not running in a recognized CI environment or git repository", ErrInvalidConfig)
	}

	a.config.logger.Info("CI environment detected",
		"provider", string(ci.Provider),
		"source", ci.Source,
		"repo", ci.Repo.Name,
		"ref", ci.Ref.Name,
		"ref_type", string(ci.Ref.Type),
	)

	// Find scanners with TargetTypeRepository (use registry keys which may be remapped)
	type repoScanner struct {
		name    string // registry key (remapped name, e.g., "custom_semgrep")
		scanner Scanner
	}
	var repoScanners []repoScanner
	for name, s := range a.scanners {
		for _, t := range s.AssetTypes() {
			if t == TargetTypeRepository {
				repoScanners = append(repoScanners, repoScanner{name: name, scanner: s})
				break
			}
		}
	}

	if len(repoScanners) == 0 {
		_ = a.tokenManager.RevokeToken(ctx)
		return fmt.Errorf("%w: no registered scanners support TargetTypeRepository", ErrInvalidConfig)
	}

	var lastErr error
	for _, rs := range repoScanners {
		if err := a.executeCIJob(ctx, ci, rs.name, rs.scanner); err != nil {
			a.config.logger.Error("CI job failed", "scanner", rs.name, "error", err)
			lastErr = err
		}
	}

	_ = a.tokenManager.RevokeToken(ctx)
	return lastErr
}

func (a *Agent) executeCIJob(ctx context.Context, ci *CIContext, scannerName string, handler Scanner) error {
	// 1. Resolve params from env vars
	params := resolveParamsFromEnv(handler.Params(), ci.Parameters)

	// 2. Create job via API
	apiReq := ciContextToAPIRequest(ci, scannerName)
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
	a.config.logger.Info("CI job created", "job_id", jobID, "scanner", scannerName)

	// 3. Build job from CIContext (no getJobDetail call needed)
	clusterInfo := a.tokenManager.GetClusterInfo()
	j := newCIJob(jobID, ci, scannerName, params, ClusterInfo{
		ID:                 clusterInfo.ID,
		Name:               clusterInfo.Name,
		AgentType:          clusterInfo.AgentType,
		Tags:               clusterInfo.Tags,
		AcceptUntaggedJobs: clusterInfo.AcceptUntaggedJobs,
		MaxConcurrentJobs:  clusterInfo.MaxConcurrentJobs,
	})

	// 3b. Attach job logger + log transport
	ciJobLogger, ciBufHandler := newJobLogger(jobID, scannerName, a.config.logger)
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

	// 4. Report started
	ciJobLogger.Info("job started", "scanner", scannerName, "ci_provider", string(ci.Provider))
	a.reportJobStarted(ctx, jobID)

	// 4b. Prepare repository (CI mode: sets repoDir from CI context, no clone)
	if err := j.(*job).prepareRepository(ctx); err != nil {
		a.reportJobFailed(ctx, jobID, fmt.Sprintf("prepare repo: %v", err))
		cancelCILog()
		ciLogWg.Wait()
		return err
	}
	defer j.(*job).cleanupRepository()

	// 5. Heartbeat loop
	hbCtx, cancelHB := context.WithCancel(ctx)
	go a.jobHeartbeatLoop(hbCtx, jobID)

	// 6. Execute scanner
	var scanErr error
	err = handler.Scan(ctx, j, func(res Result) {
		a.importResult(ctx, jobID, res)
	})
	if err != nil {
		scanErr = err
	}

	// 7. Report final status (log before stopping transport so entry gets flushed)
	if scanErr != nil {
		ciJobLogger.Error("job failed", "error", scanErr.Error())
	} else {
		ciJobLogger.Info("job completed")
	}

	// Stop log transport (triggers final flush)
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

// resolveParamsFromEnv resolves scanner params from env vars (CI mode).
// Priority: env var > CIContext.Parameters > param default.
func resolveParamsFromEnv(params []Param, ciParams map[string]interface{}) map[string]interface{} {
	resolved := make(map[string]interface{})
	for _, p := range params {
		// 1. Env var (highest priority)
		if p.envVar != "" {
			if val, ok := os.LookupEnv(p.envVar); ok {
				resolved[p.name] = val
				continue
			}
		}
		// 2. CIContext.Parameters
		if ciParams != nil {
			if val, ok := ciParams[p.name]; ok {
				resolved[p.name] = val
				continue
			}
		}
		// 3. Default
		if p.defaultVal != nil {
			resolved[p.name] = p.defaultVal
		}
	}
	return resolved
}
