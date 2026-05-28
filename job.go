package rediver

import (
	"context"
	"log/slog"

	scannerv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/scanner/v1"
	"github.com/califio/rediver-sdk-go/utils"
)

// LogLevel represents log severity.
type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// JobType indicates whether this is a discovery or retest job.
type JobType int

const (
	JobTypeDiscovery JobType = iota
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
	ID() string
	ExecutionToken() string
	Type() JobType
	Domains() []DomainTarget
	IPs() []IPTarget
	Subnets() []SubnetTarget
	Services() []ServiceTarget
	Param(name string) ParamValue
	Repository() (*Repository, bool)
	RepoDir() string
	ChangedFiles(ctx context.Context) (*utils.ChangedFiles, error)
	Integration() *Integration
	Scanner() string
	TimeoutMinutes() int
	Version() int

	// Logger returns a *slog.Logger that writes to both terminal and backend.
	Logger() *slog.Logger
}

// ArtifactDownload describes a downloadable source artifact and optional
// client-side decryption metadata.
type ArtifactDownload struct {
	PresignedURL        string
	EncryptionAlgorithm string
	EncryptionKey       string
}

// artifactDownloadFunc fetches a presigned download URL and optional
// decryption metadata for the given artifactID.
type artifactDownloadFunc func(ctx context.Context, artifactID string) (*ArtifactDownload, error)

// logFunc sends a log message to the backend via Log RPC. Injected by Agent.
type logFunc func(level LogLevel, message string)

// job is the internal implementation of Job.
type job struct {
	detail             *scannerv1.GetJobDetailResponse
	params             map[string]interface{}
	logFn              logFunc              // injected by agent_execute.go — calls Log RPC
	logLevel           slog.Level           // minimum log level for both sinks
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
			return 1
		}
		return int(v)
	}
	return 1
}

func (j *job) Logger() *slog.Logger {
	return slog.New(newJobLogHandler(j.logFn, j.logLevel))
}
