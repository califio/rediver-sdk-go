# Rediver SDK

Go SDK for building security scanners that integrate with the [Rediver](https://rediver.io) Attack Surface Management platform.

## Installation

```bash
go get github.com/califio/rediver-sdk-go
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "os/signal"

    "github.com/califio/rediver-sdk-go"
)

func main() {
    agent, err := rediver.NewAgent(
        os.Getenv("REDIVER_URL"),
        os.Getenv("REDIVER_TOKEN"),
        rediver.WithWorkerMode(),
    )
    if err != nil {
        log.Fatal(err)
    }

    scanner := rediver.NewScanner("my_scanner",
        []rediver.TargetType{rediver.TargetTypeDomain},
        scanHandler,
        rediver.WithParam(
            rediver.IntParam("threads").
                Label("Threads").
                Description("Number of concurrent threads").
                Default(10).
                Build(),
        ),
    )

    if err := agent.Register(scanner); err != nil {
        log.Fatal(err)
    }

    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
    defer cancel()

    if err := agent.Run(ctx); err != nil {
        log.Fatal(err)
    }
}

func scanHandler(ctx context.Context, job rediver.Job, emit func(rediver.Result)) error {
    threads := job.Param("threads").IntOr(10)
    fmt.Printf("Scanning with %d threads\n", threads)

    for _, target := range job.Domains() {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }

        emit(rediver.Domains(rediver.Domain{
            Domain: "sub." + target.Value,
            A:      []string{"1.2.3.4"},
        }))
    }
    return nil
}
```

## Architecture Overview

```
┌────────────────────────────────────────────────┐
│                Your Scanner Binary              │
│                                                 │
│  ┌──────────┐  ┌──────────┐  ┌──────────────┐  │
│  │ Scanner 1│  │ Scanner 2│  │  Scanner N   │  │
│  │(handler) │  │(handler) │  │  (handler)   │  │
│  └────┬─────┘  └────┬─────┘  └──────┬───────┘  │
│       └──────────────┼───────────────┘          │
│              ┌───────┴───────┐                  │
│              │     Agent     │                  │
│              │ (lifecycle,   │                  │
│              │  heartbeats,  │                  │
│              │  job dispatch)│                  │
│              └───────┬───────┘                  │
└──────────────────────┼──────────────────────────┘
                       │ HTTP API
               ┌───────┴───────┐
               │ Rediver Server│
               └───────────────┘
```

**Agent** manages the lifecycle: authentication, job polling, heartbeats, result import, and graceful shutdown. **Scanners** implement your scanning logic and emit results back to the platform.

## Run Modes

The SDK supports three run modes, each suited for different deployment strategies:

| Mode | Use case | Lifecycle |
|------|----------|-----------|
| **Worker** | Long-running agent (VM, bare-metal) | Register → poll loop → heartbeats → graceful shutdown |
| **Task** | Orchestrated containers (K8s Job) | Connect → pull 1 job → execute → revoke token → exit |
| **CI** | CI/CD pipelines (GitLab CI, GitHub Actions) | Detect env → create job → scan local repo → report → exit |

### Worker Mode

Long-running agent that continuously polls for jobs. Ideal for persistent deployments.

```go
agent, _ := rediver.NewAgent(url, token,
    rediver.WithWorkerMode(),
    rediver.WithMaxConcurrency(5),            // parallel jobs (default: 1)
    rediver.WithPollInterval(30*time.Second),  // poll frequency (default: 5s)
    rediver.WithShutdownTimeout(2*time.Minute), // graceful shutdown window
    rediver.WithAgentIDPath("/data/agent-id"), // persist agent ID across restarts
)

agent.Register(scanner1, scanner2) // register multiple scanners
agent.RunAsWorker(ctx)             // blocks until context cancelled
```

### Task Mode (Default)

Single-job execution for container orchestration. Pulls one job, executes, revokes token, and exits.

```go
agent, _ := rediver.NewAgent(url, token,
    rediver.WithTaskMode(), // default, can be omitted
)

agent.Register(scanner)
agent.Run(ctx) // execute one job then exit
```

**Direct job execution** — skip polling and run a specific job by ID:

```go
agent.Run(ctx, rediver.WithJobID("job-uuid"))
```

### CI Mode

Auto-detects CI environment (GitLab CI, GitHub Actions), creates a job on the server, scans the locally checked-out repository, and exits.

```go
agent, _ := rediver.NewAgent(url, token,
    rediver.WithCIMode(),
)

agent.Register(rediver.NewScanner("semgrep",
    []rediver.TargetType{rediver.TargetTypeRepository},
    semgrepHandler,
))

agent.RunAsCI(ctx)
```

**GitLab CI integration:**

```yaml
sast_scan:
  image: your-scanner-image:latest
  script:
    - ci-scanner
  variables:
    REDIVER_URL: https://rediver.example.com
    REDIVER_TOKEN: $CLUSTER_TOKEN
```

**GitHub Actions integration:**

```yaml
- name: SAST Scan
  run: ci-scanner
  env:
    REDIVER_URL: https://rediver.example.com
    REDIVER_TOKEN: ${{ secrets.CLUSTER_TOKEN }}
```

**Run mode via environment variable:**

Set `REDIVER_RUN_MODE` to `worker`, `task`, or `ci` instead of using option functions.

## Scanner

A scanner defines what your tool does, what target types it accepts, and its configurable parameters.

```go
scanner := rediver.NewScanner("scanner_name",
    []rediver.TargetType{rediver.TargetTypeDomain, rediver.TargetTypeIP},
    discoverHandler,
    rediver.WithDisplayName("My Scanner"),
    rediver.WithRetestHandler(retestHandler),    // optional retest logic
    rediver.WithParam(
        rediver.StringParam("wordlist").
            Label("Wordlist").
            Description("Path to wordlist file").
            Default("/usr/share/wordlists/default.txt").
            Build(),
    ),
)
```

### Handler Function

Every scanner needs a handler function with this signature:

```go
func handler(ctx context.Context, job rediver.Job, emit func(rediver.Result)) error
```

- `ctx` — cancelled on shutdown or job timeout
- `job` — targets, parameters, and metadata from Rediver
- `emit` — call to send results back to the platform (can be called multiple times)

### Target Types

Declare which asset types your scanner can process:

| Constant | Description |
|----------|-------------|
| `TargetTypeDomain` | Subdomains (e.g., `api.example.com`) |
| `TargetTypeRootDomain` | Root domains (e.g., `example.com`) |
| `TargetTypeIP` | IP addresses |
| `TargetTypeSubnet` | CIDR subnets |
| `TargetTypeService` | Host:port services |
| `TargetTypeRepository` | Git repositories (CI mode) |
| `TargetTypeASN` | ASN numbers |

### Retest Handler

Optional handler for re-validating existing assets. If not provided, retest jobs complete silently.

```go
rediver.WithRetestHandler(func(ctx context.Context, job rediver.Job, emit func(rediver.Result)) error {
    // Re-check existing assets from job.Domains(), job.Services(), etc.
    // Emit only assets that are still valid
    return nil
})
```

## Job

Jobs contain targets and parameters dispatched from Rediver.

### Accessing Targets

The main handler only receives **discovery** jobs. Retest jobs are routed to the retest handler (see [Retest Handler](#retest-handler)).

```go
// Discovery handler — receives new targets to scan
func discoverHandler(ctx context.Context, job rediver.Job, emit func(rediver.Result)) error {
    for _, domain := range job.Domains() {
        // domain.Value is the target to scan
    }

    // Other target accessors
    for _, ip := range job.IPs() { /* ip.Value */ }
    for _, subnet := range job.Subnets() { /* subnet.Value (CIDR) */ }
    for _, svc := range job.Services() { /* svc.Host, svc.Port, svc.URL */ }

    return nil
}

// Retest handler — receives existing assets to re-validate
func retestHandler(ctx context.Context, job rediver.Job, emit func(rediver.Result)) error {
    for _, domain := range job.Domains() {
        // domain.ID — existing asset ID in Rediver
        // domain.Value, domain.CNAME, domain.IPs — current known values
        // Re-resolve and emit only assets that are still valid
    }
    return nil
}
```

### Accessing Parameters

Parameters are type-safe with fallback values:

```go
threads := job.Param("threads").IntOr(10)
wordlist := job.Param("wordlist").StringOr("/default.txt")
enabled := job.Param("enabled").BoolOr(true)
tags := job.Param("tags").StringsOr([]string{"default"})

// Check if a parameter was set
if job.Param("custom").IsSet() {
    value := job.Param("custom").String()
}
```

### Repository Access (CI/SAST)

For repository-targeted scanners:

```go
func handler(ctx context.Context, job rediver.Job, emit func(rediver.Result)) error {
    // Get local repo path (CI mode uses existing checkout, others clone)
    repoDir, err := job.PrepareRepository(ctx)
    if err != nil {
        return err
    }

    // Get changed files for PR/MR scans
    changed, err := job.ChangedFiles(ctx, repoDir)
    if err != nil {
        return err
    }
    // changed.Added, changed.Modified, changed.Deleted

    // Scan files and emit findings...
    return nil
}
```

## Results

Emit results to import discovered assets and findings into Rediver.

### Domains

```go
emit(rediver.Domains(
    rediver.Domain{Domain: "api.example.com", A: []string{"1.2.3.4"}},
    rediver.Domain{Domain: "www.example.com", CNAME: "cdn.example.com"},
))
```

### Services

```go
emit(rediver.Services(
    rediver.Service{
        Host: "example.com", Port: 443, ServiceName: "https",
        Certificate: &rediver.TLSInfo{
            SubjectCN: "example.com",
            IssuerOrg: "Let's Encrypt",
            NotAfter:  "2025-06-01T00:00:00Z",
        },
        HTTP: &rediver.HTTPInfo{
            URL:          "https://example.com",
            StatusCode:   200,
            Title:        "Example Site",
            Webserver:    "nginx/1.24.0",
            Technologies: []string{"React", "Node.js"},
        },
    },
))
```

### Web Findings

```go
emit(rediver.WebFindings(
    rediver.WebFinding{
        Name:      "SQL Injection",
        Severity:  rediver.SeverityCritical,
        Endpoint:  "https://example.com/api/login",
        Category:  "injection",
        RuleID:    "sqli-001",
        CWEs:      []string{"CWE-89"},
        CVSSScore: 9.8,
        Requests: []rediver.HTTPRequest{
            {
                RawRequest:  "POST /api/login HTTP/1.1\nHost: example.com\n\n{\"user\":\"admin' OR '1'='1\"}",
                RawResponse: "HTTP/1.1 200 OK\n\n{\"success\":true}",
            },
        },
        Remediation: "Use parameterized queries.",
    },
))
```

### SAST Findings

```go
emit(rediver.SASTFindings(
    rediver.SASTFinding{
        Name:        "Hardcoded Secret",
        Severity:    rediver.SeverityHigh,
        File:        "config/settings.py",
        StartLine:   42,
        EndLine:     42,
        Snippet:     "API_KEY = 'sk-...'",
        Category:    "secret",
        RuleID:      "hardcoded-secret-001",
        CWEs:        []string{"CWE-798"},
        CVSSScore:   7.5,
        Remediation: "Use environment variables or a secrets manager.",
    },
))
```

### Severity Levels

`SeverityCritical` | `SeverityHigh` | `SeverityMedium` | `SeverityLow` | `SeverityInfo` | `SeverityNone`

## Parameters

Fluent builder API for declaring scanner parameters visible in the Rediver UI:

```go
// String
rediver.StringParam("wordlist").
    Label("Wordlist").
    Description("Path to wordlist file").
    Required().
    Default("/usr/share/wordlists/default.txt").
    Env("SCANNER_WORDLIST"). // CI mode: resolve from env var
    Build()

// Integer
rediver.IntParam("threads").Label("Threads").Default(10).Build()

// Boolean
rediver.BoolParam("aggressive").Label("Aggressive Mode").Default(false).Build()

// Float
rediver.FloatParam("threshold").Label("Score Threshold").Default(0.5).Build()

// Arrays
rediver.StringArrayParam("tags").Label("Tags").Build()
rediver.IntArrayParam("ports").Label("Ports").Build()
```

**CI mode parameter resolution order:** environment variable (`.Env()`) > CI context parameters > default value.

## Agent Options

| Option | Default | Description |
|--------|---------|-------------|
| `WithWorkerMode()` | — | Long-running daemon with poll loop |
| `WithTaskMode()` | yes | Single job, revoke token, exit |
| `WithCIMode()` | — | CI environment auto-detect |
| `WithMaxConcurrency(n)` | 1 | Max concurrent jobs (worker mode) |
| `WithPollInterval(d)` | 5s | Job poll interval (worker mode, min 1s) |
| `WithShutdownTimeout(d)` | 0 (wait forever) | Graceful shutdown window |
| `WithAgentIDPath(path)` | `~/.rediver/agent-id` | Persist agent ID across restarts |
| `WithAgentID(id)` | — | Force specific agent ID |
| `WithVersion(v)` | — | Agent version string |
| `WithHostname(h)` | — | Override hostname |
| `WithLogger(logger)` | — | Custom logger (Debug/Info/Warn/Error) |
| `WithRetryDefault()` | — | 5 attempts, exponential backoff |
| `WithRetryAggressive()` | — | 10 attempts, longer backoff |
| `WithNoRetry()` | — | Disable retries |
| `WithHTTPClient(c)` | — | Custom HTTP client |

## Utilities

```go
import "github.com/califio/rediver-sdk-go/utils"

// Execute external commands
output, err := utils.Exec(ctx, "nmap", "-sV", target)

// Git operations
repo, err := utils.GitClone(ctx, repoURL, destPath)
diff, err := utils.GitDiff(ctx, repoPath, "HEAD~1", "HEAD")

// Machine info
info := utils.GetMachineInfo()
machineID, err := utils.GetMachineID()
```

## Error Handling

The SDK provides typed errors for control flow:

```go
import "errors"

if err := agent.Run(ctx); err != nil {
    var apiErr *rediver.APIError
    if errors.As(err, &apiErr) {
        log.Printf("API error %d: %s", apiErr.StatusCode, apiErr.Message)
    }

    if errors.Is(err, rediver.ErrAuthFailed) {
        log.Fatal("invalid token")
    }
    if errors.Is(err, rediver.ErrNoJobAvailable) {
        log.Println("no jobs in queue")
    }
}
```

**Sentinel errors:** `ErrJobNotFound`, `ErrJobCancelled`, `ErrInvalidJob`, `ErrNoJobAvailable`, `ErrConnectionLost`, `ErrAuthFailed`, `ErrRateLimited`, `ErrMaxRetries`, `ErrInvalidConfig`, `ErrReregistered`

## Examples

Complete working examples in the [examples/](examples/) directory:

| Example | Mode | Description |
|---------|------|-------------|
| [subdomain](examples/subdomain/) | Worker | Subdomain enumeration with retest support |
| [serviceprobe](examples/serviceprobe/) | Worker | Service/port scanning with TLS and HTTP info |
| [vulnscan](examples/vulnscan/) | Worker | Web vulnerability scanner with finding reporting |
| [multi-scanner](examples/multi-scanner/) | Worker | Multiple scanners in a single agent |
| [task](examples/task/) | Task | Single-job execution for container orchestration |
| [direct-job](examples/direct-job/) | Task | Execute a specific job by ID |
| [ci-scanner](examples/ci-scanner/) | CI | SAST scanning in GitLab CI / GitHub Actions |

## Environment Variables

| Variable | Description |
|----------|-------------|
| `REDIVER_URL` | Rediver server URL |
| `REDIVER_TOKEN` | Agent cluster token |
| `REDIVER_RUN_MODE` | Run mode: `worker`, `task` (default), or `ci` |
| `REDIVER_JOB_ID` | Specific job ID for direct execution |

## API Client

The SDK communicates with the Rediver server using the [Connect protocol](https://connectrpc.com) (HTTP/1.1 and HTTP/2 compatible gRPC alternative). Service clients are generated from the Rediver proto definitions published on the [Buf Schema Registry](https://buf.build/rediver/api) and are consumed as Go module dependencies — no local code generation is required.

## License

Proprietary - Calif Engineering
