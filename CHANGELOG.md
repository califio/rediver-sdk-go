# Changelog

All notable changes to the Rediver SDK will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.2.5] - 2026-03-26

### Changed
- Separate `persistent` and `syncMetadata` flags in agent creation — Dispatcher mode is persistent but does not sync scanner metadata (only Worker mode syncs)

## [1.2.4] - 2026-03-26

### Added
- `RunOnce(ctx, jobID...)` accepts optional job ID to skip polling and execute a specific job directly
- `RunDirect` support via deprecated `Agent.Run(ctx, WithJobID("id"))` for backward compatibility
- `directJobID` field in runner config for direct job execution mode

## [1.2.3] - 2026-03-26

### Fixed
- Shallow clone baseline unreachable — progressively deepen (10→20→50→100) until merge-base connects HEAD to BaseCommitSHA
- Git fetch/checkout errors now include stderr output instead of just exit codes

## [1.2.2] - 2026-03-26

### Fixed
- Missing repository context in job logs — now logs URL, branch, commit, base_commit, event, and artifact_id before prepare

## [1.2.1] - 2026-03-26

### Fixed
- Scanner metadata (asset_types) not synced because generate-token response lacks scanner info — now uses scanner's own metadata directly
- Aliased `GenerateTokenResponse` to generated `api.GenerateAgentTokenResult` for type safety

### Removed
- Dead `ClusterInfo` struct and `Job.ClusterInfo()` method (never populated by backend)
- Dead `RegisteredScannerInfo` struct (unused after metadata sync fix)

## [1.2.0] - 2026-03-26

### Breaking Changes
- Removed deprecated `WithAgentID`, `WithAgentIDPath`, `Reregister`, `ErrReregistered`
- Removed `ListenForJobs` and listen-mode code
- Replaced `Agent` with `Runner` as primary public API — use `NewRunner` instead

### Added
- `Runner` as primary public API with `NewRunner` constructor
- Artifact API endpoints: presign, complete, download
- `GetArtifactPresignedURL` — returns presigned download URL for artifacts
- Per-scanner agent architecture with generate-token flow
- Auto 401 refresh via `authRetryTransport` RoundTripper middleware
- `GenerateToken` and `RunModeDispatcher` in auth package
- Comprehensive edge case unit tests and transport unit tests

### Changed
- Renamed `DoHeartbeatPing` to `AgentHeartbeat`, removed token param
- Simplified `TokenManager` internals
- Moved agent struct to internal, `Runner` wraps it as thin public API
- Regenerated API client with poll job and artifact endpoints
- Removed dead scanner name remapping logic

### Documentation
- Updated examples to use `NewRunner` API

## [1.0.3] - 2026-03-24

### Fixed
- `pullJob` now auto-re-registers on 401 response — agents no longer get stuck in infinite retry loops with expired tokens after backend downtime
- `Reregister` supports Task/CI mode via `connectFn` (was Worker-only, would fail with "register function not set")

### Added
- 8 unit tests for `Reregister` covering Worker/Task/CI modes, single-flight dedup, and error cases
- 4 integration tests with httptest mock server for `pullJob` 401 → re-register → retry flow

## [1.0.2] - 2026-03-13

### Added
- `ListenForJobs` method — run agent as a job manager that polls for jobs and dispatches them via user callback (e.g., create K8s Jobs)
- `JobHandler` type for custom job dispatch logic
- `dispatchJob` struct implementing `worker.Job` with captured `drainCtx` for graceful shutdown
- Unit tests for ListenForJobs validation, dispatchJob execution, context handling, and error paths

### Fixed
- `RunAsCI` double-CAS regression — CI mode via `Run()` no longer fails with "agent already running"
- Empty job ID guard in `pullJob` — rejects empty strings from server as `ErrNoJobAvailable`

### Changed
- `running` field changed from `bool` to `atomic.Bool` to prevent data races
- Extracted `registerAndInitPool` from `Run()` for reuse by `ListenForJobs`
- Renamed `CreateCiJob` to `CreateJob` in API client

## [1.0.1] - 2026-03-06

### Fixed
- Graceful shutdown: running jobs now continue heartbeating during pod termination
- Introduced `drainCtx` pattern — separates "stop polling" (signal ctx) from "stop running jobs" (drain ctx)
- Agent heartbeat continues during graceful shutdown drain period
- Shutdown timeout now properly cancels drain context before forcing exit, preventing deadlock

### Changed
- `runDaemon` creates separate drain context for job execution lifecycle
- Jobs submitted to worker pool use `drainCtx` instead of signal context
- `pollLoop` exits immediately on SIGTERM; no new jobs pulled after shutdown signal

## [1.0.0] - 2026-03-05

### Added
- Initial public release of Rediver Go SDK
- Agent lifecycle management (registration, heartbeats, graceful shutdown)
- Three run modes: Worker (long-running), Task (single-job), CI (pipeline integration)
- Scanner abstraction with handler functions and typed result emission
- Target types: Domain, RootDomain, IP, Subnet, Service, Repository, ASN
- Result types: Domains, Services, WebFindings, SASTFindings
- Type-safe parameter builder (String, Int, Bool, Float, Arrays)
- CI environment auto-detection (GitLab CI, GitHub Actions)
- Local git context detection for non-CI environments
- Repository management with auto-clone and cleanup
- Configurable retry strategies (default, aggressive, none)
- Structured logging with custom logger support
- Utility functions for command execution, git operations, and machine info
