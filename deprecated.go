package rediver

// Deprecated API surface — kept for backward compatibility.
// Will be removed in a future major version.
//
// Migration guide:
//   NewAgent(url, token, opts...)  → NewRunner(url, token, opts...)
//   agent.Register(scanners...)    → runner.Add(scanners...)
//   agent.Run(ctx)                 → runner.Run(ctx)
//   agent.RunAsWorker(ctx)         → runner.Run(ctx) with WithWorkerMode()
//   agent.RunAsTask(ctx, opts...)  → runner.RunOnce(ctx)
//   agent.RunAsCI(ctx)             → runner.RunCI(ctx)
//   agent.ListenForJobs(...)       → runner.Dispatch(ctx, handler) — REMOVED
