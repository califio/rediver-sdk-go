# Changelog

All notable changes to the Rediver SDK will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).


## [Unreleased]

## [1.3.1] - 2026-05-05

### Fixed

- CI repository scans now expose the generated agent token through `Job.ExecutionToken()` before invoking scanner code, matching task/direct job behavior and allowing scanners to authenticate job-scoped backend resources such as the LLM gateway.

### Tests

- Added CI-mode coverage asserting `RunCI` passes the execution token to scanner handlers.

## [1.3.0] - 2026-05-04

> **Note on versioning:** This release contains substantial API changes that strict SemVer would classify as a major bump. Released as `1.3.0` because all in-tree consumers (rediver scanners, dispatcher) are migrated to the new API in lockstep — no out-of-tree consumers exist. External adopters should treat `v1.3.0` as a hard breaking-change boundary.

### Breaking changes

#### Agent API

- `Runner` type removed. Use `Agent` instead.
- `NewRunner(url, token, opts...)` -> `NewAgent(token, scanner, opts...)`. Server URL now defaults to `https://api.rediver.ai`; override via `WithServerURL` option or `REDIVER_URL` env.
- `Runner.Add(scanners...)` removed. Pass the scanner directly to `NewAgent`.
- Deprecated `Agent`/`NewAgent` wrapper removed. Reclaimed name now points to the new single-scanner type.
- Run-mode options removed: `WithWorkerMode()`, `WithTaskMode()`, `WithCIMode()`, `WithJobID()`. Use the explicit lifecycle methods instead.
- `RunMode` type and `RunModeWorker/Task/CI/Dispatcher` constants removed.
- `RunOption` type removed (was only used by `WithJobID`).
- `REDIVER_RUN_MODE` env no longer read by SDK. Scanner mains read their own env.
- `examples/multi-scanner/` removed. Run multiple scanners with multiple Agent instances + caller-side errgroup.

#### Job event API

- `Job.Logger() *slog.Logger` removed from the `Job` interface and `*job` struct. Use `job.Emit(rediver.NewLog(level, msg))` for direct event emission, or `slog.New(job.SlogHandler())` when interop with `*slog.Logger` is required.
- Internal job-scoped `logger` field removed from `*job`.

#### Transport (internal)

- Replaced oapi-codegen REST client with Connect-protocol service clients distributed via the Buf Schema Registry (`buf.build/rediver/api`).
- `internal/transport.Client` now wraps Connect service clients (`TokenService`, `AgentService`, `JobService`, `ArtifactService`, `FindingService`, `AssetService`).
- Enum types (`Severity`, `TargetType`, `GitProvider`) now independent string types with internal proto conversion; no longer type aliases of generated REST client types.

### Migration

```go
// Logger() removal
// Before
log := job.Logger()
log.Info("scanning", "target", target)

// After (option A — direct event)
job.Emit(rediver.NewLog(rediver.LogLevelInfo,
    fmt.Sprintf("scanning target=%s", target)))

// After (option B — keep slog for helpers that take *slog.Logger)
log := slog.New(job.SlogHandler())
log.Info("scanning", "target", target)
```

```go
// Runner -> Agent
// Before
runner := rediver.NewRunner(url, token, opts...)
runner.Add(scanner)
runner.Run(ctx)

// After
agent, _ := rediver.NewAgent(token, scanner, rediver.WithServerURL(url))
agent.Run(ctx)
```

### Added

- `NewAgent(token, scanner, opts...)` — single-scanner constructor.
- `Agent.Run(ctx)`, `Agent.RunOnce(ctx, jobID...)`, `Agent.RunCI(ctx)`, `Agent.Dispatch(ctx, handler)`, `Agent.Stop()` — explicit lifecycle methods.
- `WithServerURL(url)` option + `DefaultServerURL` constant (`"https://api.rediver.ai"`).
- `ErrAlreadyRunning` sentinel for one-shot guard.
- `Job.Emit(event)` + `Job.SlogHandler()` — unified event API.
- `JobEvent` core types + `IsEphemeral` classifier; constructors for `NewLog`, ToolUse, Text/Thinking deltas.
- `eventTransport` with channel + flush worker + sequence assignment.
- Long-polling dispatch mode with `DispatchMode`.
- `internal/connectclient` — lightweight Connect service client wrapper.
- `internal/transport.authRetryTransport` — single-flight 401 refresh.
- Direct dependencies: `connectrpc.com/connect`, `buf.build/gen/go/rediver/api/connectrpc/go`, `buf.build/gen/go/rediver/api/protocolbuffers/go`, `google.golang.org/protobuf`.
- `Agent.detectGitContext` honors configured `repoDir` for local git scans outside CI environments (auto-skipped when `GITLAB_CI` / `GITHUB_ACTIONS` set).

### Removed

- `internal/api/client.gen.go` (5 889-line oapi-codegen-generated) and `oapi-codegen.yaml`.
- `github.com/deepmap/oapi-codegen` and transitive dependencies.
- Legacy `log_transport` / `job_logger` files.

### Internal

- `agent.go` (805 lines) split into 11 focused per-mode files.
- `job.go` (741 lines) split into 4 focused files.
- All package files now under 200 lines.
- `agent_generate_token_test.go` and `dispatcher_metadata_smoke_test.go` rewritten on in-process Connect httptest server.

## [1.2.10] - 2026-04-03

### Added
- `Job.ExecutionToken()` so scanners can read the current execution token snapshot for the running job

### Changed
- The SDK now attaches the current execution token to each job before invoking the scanner handler

### Tests
- Added coverage for `Job.ExecutionToken()` on empty and populated jobs

## [1.2.9] - 2026-04-03

### Fixed
- Direct task execution now passes `job_id` to `/api/agent/generate-token` when using `RunOnce(ctx, jobID)` so backend can mint a job-scoped token for the requested job
- Task mode without a direct `jobID` keeps the previous generate-token behavior and does not send `job_id`

### Changed
- Regenerated the generated API client from backend swagger so `GenerateAgentTokenRequest` includes `job_id`

### Tests
- Added coverage for direct task generate-token requests with and without `job_id`

## [1.2.8] - 2026-04-02

### Added
- `WithDispatcherMetadataSync()` option to let Dispatcher mode sync scanner metadata on startup when the dispatcher owns scanner config
- `WithRawParamsSchema(...)` scanner option to send raw `params_schema` JSON to backend metadata sync instead of deriving from `Params()`
- Dispatcher metadata smoke test covering generate-token, `/api/agent/scanner` PATCH, `X-Token`, `asset_types`, and `params_schema`

### Changed
- Metadata sync now prefers a scanner-provided raw params schema before falling back to schema generation from typed params
- Dispatcher mode can opt into metadata sync without changing existing default behavior

## [1.2.7] - 2026-03-30

### Fixed
- Resolve HEAD SHA after clone for jobs without CommitSHA — `git rev-parse HEAD` fills CommitSHA when server provides empty value (e.g., manual triggers on connector-synced repos)
- Per-finding CommitSha now populated with resolved HEAD instead of staying empty

### Added
- `GitRevParseHead` utility in `utils/git.go`

### Tests
- `resolvedHeadSHA` population and override behavior

## [1.2.6] - 2026-03-30

### Fixed
- Fallback to branch checkout when CommitSHA is empty — asset-synced refs without commit info now fetch and checkout `origin/<branch>` instead of failing with `git checkout ""`

### Tests
- `buildRefSpecs` empty CommitSHA with branch fallback (2 cases)

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
