package rediver

import "context"

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
func (a *Agent) Run(ctx context.Context, _ ...RunOption) error {
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
