package rediver

import (
	"context"
	"log/slog"

	agentv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/agent/v1"
	"github.com/califio/rediver-sdk-go/utils"
)

// JobType indicates whether this is a discovery or retest job.
type JobType int

const (
	// JobTypeDiscovery is for discovering new assets from targets.
	JobTypeDiscovery JobType = iota
	// JobTypeRetest is for validating existing assets.
	JobTypeRetest
)

func (t JobType) String() string {
	switch t {
	case JobTypeDiscovery:
		return "discovery"
	case JobTypeRetest:
		return "retest"
	default:
		return "unknown"
	}
}

// Job provides access to scan job details.
type Job interface {
	// ID returns the unique job identifier.
	ID() string

	// ExecutionToken returns the current execution token snapshot for this job.
	ExecutionToken() string

	// Type returns whether this is a discovery or retest job.
	Type() JobType

	// Domains returns domain targets from the job.
	Domains() []DomainTarget

	// IPs returns IP targets from the job.
	IPs() []IPTarget

	// Subnets returns subnet targets from the job.
	Subnets() []SubnetTarget

	// Services returns service targets from the job.
	Services() []ServiceTarget

	// Param returns a typed parameter accessor.
	Param(name string) ParamValue

	// Repository returns git repository info (for CI/SAST jobs).
	Repository() (*Repository, bool)

	// RepoDir returns the path to the repository working directory.
	RepoDir() string

	// ChangedFiles returns files changed between base and head commits.
	ChangedFiles(ctx context.Context) (*utils.ChangedFiles, error)

	// Integration returns third-party integration tokens for this job.
	Integration() *Integration

	// Scanner returns the scanner name this job is assigned to.
	Scanner() string

	// TimeoutMinutes returns the job timeout in minutes.
	TimeoutMinutes() int

	// Version returns the schema version of the job detail.
	Version() int

	// Logger returns a job-scoped logger for structured logging during execution.
	Logger() *slog.Logger
}

// artifactDownloadFunc fetches a presigned download URL for the given artifactID.
type artifactDownloadFunc func(ctx context.Context, artifactID string) (string, error)

// job is the internal implementation of Job.
// detail is the proto GetJobDetailResponse; ciContext is non-nil in CI mode.
type job struct {
	detail             *agentv1.GetJobDetailResponse
	params             map[string]interface{}
	ciContext          *CIContext           // non-nil = CI mode
	logger             *slog.Logger         // job-scoped logger
	executionToken     string               // snapshot for scanner subprocesses
	repoDir            string               // prepared repo path
	clonedRepoDir      string               // non-empty when SDK cloned it
	resolvedBaseSHA    string               // resolved via git merge-base
	resolvedHeadSHA    string               // resolved via git rev-parse HEAD
	artifactDownloadFn artifactDownloadFunc // injected by Agent
}

func (j *job) ID() string {
	if j.detail != nil {
		return j.detail.GetId()
	}
	return ""
}

func (j *job) ExecutionToken() string {
	return j.executionToken
}

func (j *job) Type() JobType {
	if j.detail != nil && j.detail.GetRetest() {
		return JobTypeRetest
	}
	return JobTypeDiscovery
}

func (j *job) Param(name string) ParamValue {
	if j.params == nil {
		return &paramValue{}
	}
	if val, ok := j.params[name]; ok {
		return &paramValue{value: val, set: true}
	}
	return &paramValue{}
}

func (j *job) Scanner() string {
	if j.detail != nil {
		return j.detail.GetScanner()
	}
	return ""
}

func (j *job) TimeoutMinutes() int {
	if j.detail != nil {
		return int(j.detail.GetTimeoutMinutes())
	}
	return 0
}

func (j *job) Version() int {
	if j.detail != nil {
		v := j.detail.GetVersion()
		if v == 0 {
			return 1 // default version
		}
		return int(v)
	}
	return 1
}

func (j *job) Logger() *slog.Logger {
	if j.logger == nil {
		return slog.New(discardHandler{})
	}
	return j.logger
}

// discardHandler is a slog.Handler that silently discards all records.
type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (d discardHandler) WithAttrs([]slog.Attr) slog.Handler      { return d }
func (d discardHandler) WithGroup(string) slog.Handler           { return d }
