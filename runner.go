package rediver

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
)

// Runner manages the lifecycle of one or more scanners, each backed by an internal agent.
// It is the primary public API for the Rediver SDK.
//
// Usage:
//
//	r := rediver.NewRunner(serverURL, clusterToken, rediver.WithMaxConcurrency(5))
//	r.Add(myScanner)
//	err := r.Run(ctx)
type Runner struct {
	serverURL    string
	clusterToken string
	config       *runnerConfig
	scanners     map[string]Scanner // lowercase name -> Scanner
	mu           sync.Mutex
	cancelAll    context.CancelFunc
}

// NewRunner creates a Runner with the given server URL and cluster token.
// serverURL and clusterToken are resolved from REDIVER_URL / REDIVER_TOKEN env vars
// if the arguments are empty strings.
func NewRunner(serverURL, clusterToken string, opts ...Option) (*Runner, error) {
	if serverURL == "" {
		if env := resolveServerURL(""); env != "" {
			serverURL = env
		} else {
			return nil, fmt.Errorf("%w: server URL is required (set REDIVER_URL or pass as argument)", ErrInvalidConfig)
		}
	}
	if clusterToken == "" {
		if env := resolveClusterToken(""); env != "" {
			clusterToken = env
		} else {
			return nil, fmt.Errorf("%w: cluster token is required (set REDIVER_TOKEN or pass as argument)", ErrInvalidConfig)
		}
	}

	config := defaultAgentConfig()
	for _, opt := range opts {
		opt(config)
	}

	return &Runner{
		serverURL:    serverURL,
		clusterToken: clusterToken,
		config:       config,
		scanners:     make(map[string]Scanner),
	}, nil
}

// Add registers one or more scanners with the Runner.
// Returns an error if a scanner name is empty or already registered.
// Scanner names are normalized to lowercase.
// Must be called before Run().
func (r *Runner) Add(scanners ...Scanner) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, s := range scanners {
		name := strings.ToLower(s.Name())
		if name == "" {
			return fmt.Errorf("%w: scanner name cannot be empty", ErrInvalidConfig)
		}
		if _, exists := r.scanners[name]; exists {
			return fmt.Errorf("%w: duplicate scanner name: %q", ErrInvalidConfig, name)
		}
		r.scanners[name] = s
	}
	return nil
}

// Run starts all registered scanners. The run mode is determined by the config:
//   - RunModeWorker (default): long-running poll loop per scanner
//   - RunModeTask: poll one job per scanner, execute, then exit
//   - RunModeCI: detect CI environment, create job, execute, then exit
//   - RunModeDispatcher: poll jobs and forward to a JobHandler (set via Dispatch)
//
// Blocks until ctx is cancelled, all scanners finish, or a fatal error occurs.
func (r *Runner) Run(ctx context.Context) error {
	if len(r.scanners) == 0 {
		return fmt.Errorf("%w: at least one scanner must be added before Run()", ErrInvalidConfig)
	}

	runCtx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.cancelAll = cancel
	r.mu.Unlock()
	defer cancel()

	switch r.config.runMode {
	case RunModeWorker:
		return r.runWorker(runCtx)
	case RunModeTask:
		return r.runTask(runCtx)
	case RunModeCI:
		return r.runCI(runCtx)
	case RunModeDispatcher:
		handler := r.config.jobHandler
		if handler == nil {
			return fmt.Errorf("%w: job handler must be set for Dispatcher mode (use Dispatch)", ErrInvalidConfig)
		}
		return r.runDispatcher(runCtx, handler)
	default:
		return r.runWorker(runCtx)
	}
}

// RunOnce sets Task mode and calls Run. Each scanner polls once, executes a job, revokes token, exits.
// If jobID is provided, skip polling and execute that job directly.
func (r *Runner) RunOnce(ctx context.Context, jobID ...string) error {
	r.config.runMode = RunModeTask
	if len(jobID) > 0 && jobID[0] != "" {
		r.config.directJobID = jobID[0]
	}
	return r.Run(ctx)
}

// RunCI sets CI mode and calls Run. Each scanner detects CI environment, creates a job, scans, exits.
func (r *Runner) RunCI(ctx context.Context) error {
	r.config.runMode = RunModeCI
	return r.Run(ctx)
}

// Dispatch sets Dispatcher mode and calls Run. Each scanner polls for jobs and calls handler instead
// of executing internally. The handler is responsible for dispatching the job to an external worker.
func (r *Runner) Dispatch(ctx context.Context, handler JobHandler) error {
	if handler == nil {
		return fmt.Errorf("%w: handler must be non-nil", ErrInvalidConfig)
	}
	r.config.runMode = RunModeDispatcher
	r.config.jobHandler = handler
	return r.Run(ctx)
}

// Stop cancels all running agents simultaneously. In-flight jobs drain before Run returns.
func (r *Runner) Stop() {
	r.mu.Lock()
	cancel := r.cancelAll
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// --- Internal run mode implementations ---

// runWorker creates persistent agents (Worker mode) and runs them in parallel.
// Fails fast: if any agent fails to initialize, Run returns immediately.
func (r *Runner) runWorker(ctx context.Context) error {
	agents, err := r.createAgents(ctx, true, true) // persistent + syncMetadata
	if err != nil {
		return err
	}

	r.config.logger.Info("all agents initialized (worker mode)", "count", len(agents))

	g, gCtx := errgroup.WithContext(ctx)
	for _, a := range agents {
		ag := a
		g.Go(func() error { return ag.run(gCtx) })
	}
	return g.Wait()
}

// runTask creates ephemeral agents (Task mode) and runs each once.
func (r *Runner) runTask(ctx context.Context) error {
	agents, err := r.createAgents(ctx, false, false) // ephemeral, no sync
	if err != nil {
		return err
	}

	r.config.logger.Info("all agents initialized (task mode)", "count", len(agents))

	g, gCtx := errgroup.WithContext(ctx)
	for _, a := range agents {
		ag := a
		g.Go(func() error { return ag.runOnce(gCtx) })
	}
	return g.Wait()
}

// runCI creates ephemeral agents (CI mode) and runs each in CI mode.
func (r *Runner) runCI(ctx context.Context) error {
	agents, err := r.createAgents(ctx, false, false) // ephemeral, no sync
	if err != nil {
		return err
	}

	r.config.logger.Info("all agents initialized (CI mode)", "count", len(agents))

	g, gCtx := errgroup.WithContext(ctx)
	for _, a := range agents {
		ag := a
		g.Go(func() error { return ag.runCI(gCtx) })
	}
	return g.Wait()
}

// runDispatcher creates persistent agents (Dispatcher mode) and runs each in dispatch mode.
func (r *Runner) runDispatcher(ctx context.Context, handler JobHandler) error {
	agents, err := r.createAgents(ctx, true, r.config.syncMetadataDispatcher)
	if err != nil {
		return err
	}

	r.config.logger.Info("all agents initialized (dispatcher mode)", "count", len(agents))

	g, gCtx := errgroup.WithContext(ctx)
	for _, a := range agents {
		ag := a
		g.Go(func() error { return ag.runDispatcher(gCtx, handler) })
	}
	return g.Wait()
}

// createAgents generates tokens for all scanners sequentially (fail fast) and returns agents.
// persistent: save agent token to Agents table (long-running modes)
// syncMetadata: update scanner config on backend on startup
func (r *Runner) createAgents(ctx context.Context, persistent, syncMetadata bool) ([]*agent, error) {
	agents := make([]*agent, 0, len(r.scanners))
	for _, s := range r.scanners {
		a, err := newAgent(ctx, s, r.clusterToken, r.serverURL, persistent, syncMetadata, r.config)
		if err != nil {
			return nil, fmt.Errorf("initialize scanner %q: %w", s.Name(), err)
		}
		agents = append(agents, a)
	}
	return agents, nil
}

// --- Deprecated Agent wrapper ---

// Agent is a deprecated wrapper around Runner.
// Deprecated: Use Runner instead. Will be removed in a future major version.
type Agent struct {
	runner *Runner
}

// NewAgent creates an Agent backed by a Runner.
// Deprecated: Use NewRunner instead.
func NewAgent(serverURL, clusterToken string, opts ...Option) (*Agent, error) {
	r, err := NewRunner(serverURL, clusterToken, opts...)
	if err != nil {
		return nil, err
	}
	return &Agent{runner: r}, nil
}

// Register adds scanners to the agent.
// Deprecated: Use Runner.Add instead.
func (a *Agent) Register(scanners ...Scanner) error {
	return a.runner.Add(scanners...)
}

// Run starts the agent lifecycle based on configured run mode.
// Deprecated: Use Runner.Run instead.
func (a *Agent) Run(ctx context.Context, opts ...RunOption) error {
	cfg := &runConfig{}
	for _, o := range opts {
		o(cfg)
	}
	if cfg.jobID != "" {
		return a.runner.RunOnce(ctx, cfg.jobID)
	}
	return a.runner.Run(ctx)
}

// RunAsWorker runs in worker mode.
// Deprecated: Use Runner.Run with WithWorkerMode() instead.
func (a *Agent) RunAsWorker(ctx context.Context) error {
	a.runner.config.runMode = RunModeWorker
	return a.runner.Run(ctx)
}

// RunAsTask runs in task mode.
// Deprecated: Use Runner.RunOnce instead.
func (a *Agent) RunAsTask(ctx context.Context, _ ...RunOption) error {
	return a.runner.RunOnce(ctx)
}

// RunAsCI runs in CI mode.
// Deprecated: Use Runner.RunCI instead.
func (a *Agent) RunAsCI(ctx context.Context) error {
	return a.runner.RunCI(ctx)
}

// --- Env helpers ---

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
