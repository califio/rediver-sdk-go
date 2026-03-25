package rediver

// This file contains deprecated API surface kept for backward compatibility.
// All deprecated types and functions delegate to the new Runner API.
// They will be removed in a future major version.
//
// Migration guide:
//   NewAgent(url, token, opts...) → NewRunner(url, token, opts...)
//   agent.Register(scanners...)  → runner.Add(scanners...)
//   agent.Run(ctx)               → runner.Run(ctx)
//   agent.RunAsWorker(ctx)       → runner.Run(ctx) with WithWorkerMode()
//   agent.RunAsTask(ctx)         → runner.RunOnce(ctx)
//   agent.RunAsCI(ctx)           → runner.RunCI(ctx)
//   agent.ListenForJobs(...)     → runner.Dispatch(ctx, handler) — REMOVED, new signature
