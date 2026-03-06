# Changelog

All notable changes to the Rediver SDK will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
