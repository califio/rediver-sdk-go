# Changelog

All notable changes to the Rediver SDK will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
