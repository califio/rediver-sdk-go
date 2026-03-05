package rediver

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"

	"github.com/califio/rediver-sdk-go/internal/api"
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

// ClusterInfo holds cluster metadata from registration.
// Accessible via job.ClusterInfo() for scanner-side routing decisions.
type ClusterInfo struct {
	ID                 string
	Name               string
	AgentType          string
	Tags               []string
	AcceptUntaggedJobs bool
	MaxConcurrentJobs  int
}

// Integration holds third-party integration tokens for the job.
type Integration struct {
	CloudflareTokens []string
}

// Repository contains git repository information for CI/SAST jobs.
type Repository struct {
	URL           string
	Ref           string
	RefType       string
	BaseRef       string
	RefSpecs      []string
	Depth         int
	CommitSHA     string
	BaseCommitSHA string
	Username      string
	Password      string
}

// DomainTarget contains domain info from the job target.
// ID is non-empty when the asset already exists in the database.
type DomainTarget struct {
	ID    string
	Value string
	CNAME string
	IPs   []string
}

// IPTarget contains IP address info from the job target.
// ID is non-empty when the asset already exists in the database.
type IPTarget struct {
	ID    string
	Value string
}

// SubnetTarget contains subnet info from the job target.
// ID is non-empty when the asset already exists in the database.
type SubnetTarget struct {
	ID    string
	Value string
}

// ServiceTarget contains service endpoint info from the job target.
// ID is non-empty when the asset already exists in the database.
type ServiceTarget struct {
	ID    string
	Value string
	Host  string
	Port  int
	URL   string
}

// Job provides access to scan job details.
type Job interface {
	// ID returns the unique job identifier.
	ID() string

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
	// Returns nil, false if no repository target is present.
	Repository() (*Repository, bool)

	// RepoDir returns the path to the repository working directory.
	// In CI mode, this is the CI-provided checkout directory.
	// In worker mode, this is a temp directory cloned by the SDK.
	// Returns empty string if no repository target or not yet prepared.
	RepoDir() string

	// ChangedFiles returns files changed between base and head commits.
	// Only valid for repository jobs with diff information.
	ChangedFiles(ctx context.Context) (*utils.ChangedFiles, error)

	// ClusterInfo returns metadata about the cluster this agent belongs to.
	ClusterInfo() ClusterInfo

	// Integration returns third-party integration tokens for this job.
	// Returns nil if no integrations are configured.
	Integration() *Integration

	// Scanner returns the scanner name this job is assigned to.
	Scanner() string

	// TimeoutMinutes returns the job timeout in minutes.
	TimeoutMinutes() int

	// Version returns the schema version of the job detail.
	Version() int

	// Logger returns a job-scoped logger for structured logging during execution.
	// Log entries are captured and sent to the backend for viewing in the dashboard.
	// Do NOT log passwords, tokens, or other credentials.
	Logger() *slog.Logger
}

// job is the internal implementation of Job.
type job struct {
	detail      *api.JobDetail
	params      map[string]interface{}
	clusterInfo ClusterInfo
	ciContext     *CIContext   // non-nil = CI mode
	logger        *slog.Logger // job-scoped logger (multi-handler: console + buffer)
	repoDir       string       // prepared repo path (CI dir or cloned temp dir)
	clonedRepoDir string       // non-empty only when SDK cloned it → needs cleanup
}

func newJob(detail *api.JobDetail, ci ...ClusterInfo) Job {
	j := &job{detail: detail}
	if len(ci) > 0 {
		j.clusterInfo = ci[0]
	}
	if detail != nil {
		j.resolveParams()
	}
	return j
}

// newCIJob creates a Job from CIContext data. No api.JobDetail needed.
// The job ID comes from the create-job API response.
func newCIJob(jobID string, ci *CIContext, scannerName string, params map[string]interface{}, clusterInfo ClusterInfo) Job {
	// Build a minimal JobDetail for compatibility with existing Job interface methods
	detail := &api.JobDetail{
		Id:      &jobID,
		Scanner: &scannerName,
	}
	// Set repository target from CIContext
	refType := "Branch"
	switch ci.Ref.Type {
	case CIRefTypeTag:
		refType = "Tag"
	case CIRefTypePRMR:
		refType = "PR_MR"
	}
	detail.Target = &api.JobTarget{
		Repository: &api.RepositoryContext{
			Url:           &ci.Repo.URL,
			Branch:        &ci.Ref.Branch,
			Event:         &refType,
			BaseBranch:    nilIfEmpty(ci.Ref.BaseBranch),
			CommitSha:     &ci.Ref.CommitSHA,
			BaseCommitSha: nilIfEmpty(ci.Ref.BaseCommitSHA),
		},
	}
	if len(params) > 0 {
		detail.Params = &params
	}
	j := &job{
		detail:      detail,
		clusterInfo: clusterInfo,
		ciContext:   ci,
		params:      params,
	}
	return j
}

// resolveParams copies the params map from the job detail.
func (j *job) resolveParams() {
	if j.detail.Params == nil {
		return
	}
	j.params = *j.detail.Params
}

func (j *job) ID() string {
	if j.detail != nil && j.detail.Id != nil {
		return *j.detail.Id
	}
	return ""
}

func (j *job) Type() JobType {
	if j.detail != nil && j.detail.Retest != nil && *j.detail.Retest {
		return JobTypeRetest
	}
	return JobTypeDiscovery
}

func (j *job) Targets() []string {
	var targets []string
	for _, d := range j.Domains() {
		targets = append(targets, d.Value)
	}
	for _, ip := range j.IPs() {
		targets = append(targets, ip.Value)
	}
	for _, s := range j.Subnets() {
		targets = append(targets, s.Value)
	}
	for _, s := range j.Services() {
		if s.URL != "" {
			targets = append(targets, s.URL)
		} else {
			targets = append(targets, fmt.Sprintf("%s:%d", s.Host, s.Port))
		}
	}
	if len(targets) == 0 {
		return nil
	}
	return targets
}

func (j *job) Domains() []DomainTarget {
	if j.detail == nil || j.detail.Target == nil || j.detail.Target.Domains == nil {
		return nil
	}
	domains := *j.detail.Target.Domains
	result := make([]DomainTarget, 0, len(domains))
	for _, d := range domains {
		dt := DomainTarget{}
		if d.Id != nil {
			dt.ID = *d.Id
		}
		if d.Value != nil {
			dt.Value = *d.Value
		}
		if d.Cname != nil {
			dt.CNAME = *d.Cname
		}
		if d.Ips != nil {
			dt.IPs = *d.Ips
		}
		result = append(result, dt)
	}
	return result
}

func (j *job) IPs() []IPTarget {
	if j.detail == nil || j.detail.Target == nil || j.detail.Target.Ips == nil {
		return nil
	}
	ips := *j.detail.Target.Ips
	result := make([]IPTarget, 0, len(ips))
	for _, ip := range ips {
		it := IPTarget{}
		if ip.Id != nil {
			it.ID = *ip.Id
		}
		if ip.Value != nil {
			it.Value = *ip.Value
		}
		result = append(result, it)
	}
	return result
}

func (j *job) Subnets() []SubnetTarget {
	if j.detail == nil || j.detail.Target == nil || j.detail.Target.Subnets == nil {
		return nil
	}
	subnets := *j.detail.Target.Subnets
	result := make([]SubnetTarget, 0, len(subnets))
	for _, s := range subnets {
		st := SubnetTarget{}
		if s.Id != nil {
			st.ID = *s.Id
		}
		if s.Value != nil {
			st.Value = *s.Value
		}
		result = append(result, st)
	}
	return result
}

func (j *job) Services() []ServiceTarget {
	if j.detail == nil || j.detail.Target == nil || j.detail.Target.Services == nil {
		return nil
	}
	services := *j.detail.Target.Services
	result := make([]ServiceTarget, 0, len(services))
	for _, s := range services {
		st := ServiceTarget{}
		if s.Id != nil {
			st.ID = *s.Id
		}
		if s.Value != nil {
			st.Value = *s.Value
		}
		if s.Host != nil {
			st.Host = *s.Host
		}
		if s.Port != nil {
			st.Port = int(*s.Port)
		}
		if s.Url != nil {
			st.URL = *s.Url
		}
		result = append(result, st)
	}
	return result
}

func (j *job) URLs() []string {
	services := j.Services()
	if len(services) == 0 {
		return nil
	}
	var urls []string
	for _, s := range services {
		if s.URL != "" {
			urls = append(urls, s.URL)
		}
	}
	return urls
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

func (j *job) Repository() (*Repository, bool) {
	if j.detail == nil || j.detail.Target == nil || j.detail.Target.Repository == nil {
		return nil, false
	}
	r := j.detail.Target.Repository
	repo := &Repository{}
	if r.Url != nil {
		repo.URL = *r.Url
	}
	if r.Branch != nil {
		repo.Ref = *r.Branch
	}
	if r.Event != nil {
		repo.RefType = *r.Event
	}
	if r.BaseBranch != nil {
		repo.BaseRef = *r.BaseBranch
	}
	if r.RefSpecs != nil {
		repo.RefSpecs = *r.RefSpecs
	}
	if r.Depth != nil {
		repo.Depth = int(*r.Depth)
	}
	if r.CommitSha != nil {
		repo.CommitSHA = *r.CommitSha
	}
	if r.BaseCommitSha != nil {
		repo.BaseCommitSHA = *r.BaseCommitSha
	}
	if r.Credential != nil {
		if r.Credential.Username != nil {
			repo.Username = *r.Credential.Username
		}
		if r.Credential.Password != nil {
			repo.Password = *r.Credential.Password
		}
	}
	return repo, true
}

func (j *job) RepoDir() string {
	return j.repoDir
}

// prepareRepository sets up the repo directory for scanning.
// CI mode: uses CI-provided checkout dir. Worker mode: clones to temp dir.
func (j *job) prepareRepository(ctx context.Context) error {
	if j.ciContext != nil {
		j.repoDir = j.ciContext.RepoDir
		return nil
	}

	repo, ok := j.Repository()
	if !ok {
		return fmt.Errorf("no repository target")
	}

	repoURL, err := j.buildRepoURL(repo)
	if err != nil {
		return err
	}

	workDir, err := os.MkdirTemp(os.TempDir(), "repo_")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}

	err = utils.GitCheckout(ctx, utils.CheckoutOptions{
		WorkDir:     workDir,
		RepoURL:     repoURL,
		Refs:        repo.RefSpecs,
		CheckoutRef: repo.CommitSHA,
	})
	if err != nil {
		os.RemoveAll(workDir)
		return fmt.Errorf("checkout: %w", err)
	}

	j.repoDir = workDir
	j.clonedRepoDir = workDir // track for cleanup
	return nil
}

// cleanupRepository removes the cloned repo directory if the SDK created it.
// No-op for CI mode where the repo is managed externally.
func (j *job) cleanupRepository() {
	if j.clonedRepoDir == "" {
		return
	}
	if err := os.RemoveAll(j.clonedRepoDir); err != nil {
		j.Logger().Warn("cleanup repo failed", "dir", j.clonedRepoDir, "error", err)
	} else {
		j.Logger().Info("cleaned up cloned repo", "dir", j.clonedRepoDir)
	}
	j.clonedRepoDir = ""
	j.repoDir = ""
}

func (j *job) buildRepoURL(repo *Repository) (string, error) {
	if repo.URL == "" {
		return "", fmt.Errorf("repo URL is empty")
	}

	u, err := url.Parse(repo.URL)
	if err != nil {
		return "", fmt.Errorf("invalid repo URL: %w", err)
	}

	if repo.Username != "" && repo.Password != "" {
		u.User = url.UserPassword(repo.Username, repo.Password)
	}

	return u.String(), nil
}

func (j *job) ChangedFiles(ctx context.Context) (*utils.ChangedFiles, error) {
	repo, ok := j.Repository()
	if !ok {
		return nil, fmt.Errorf("no repository target")
	}

	if repo.BaseCommitSHA == "" || repo.CommitSHA == "" {
		return nil, nil
	}

	if j.repoDir == "" {
		return nil, fmt.Errorf("repository not prepared")
	}

	return utils.GitDiff(ctx, j.repoDir, repo.BaseCommitSHA, repo.CommitSHA)
}

func (j *job) ClusterInfo() ClusterInfo {
	return j.clusterInfo
}

func (j *job) Integration() *Integration {
	if j.detail == nil || j.detail.Integration == nil {
		return nil
	}
	result := &Integration{}
	if j.detail.Integration.CloudflareTokens != nil {
		result.CloudflareTokens = *j.detail.Integration.CloudflareTokens
	}
	return result
}

func (j *job) Scanner() string {
	if j.detail != nil && j.detail.Scanner != nil {
		return *j.detail.Scanner
	}
	return ""
}

func (j *job) TimeoutMinutes() int {
	if j.detail != nil && j.detail.TimeoutMinutes != nil {
		return int(*j.detail.TimeoutMinutes)
	}
	return 0
}

func (j *job) Version() int {
	if j.detail != nil && j.detail.Version != nil {
		return int(*j.detail.Version)
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
func (d discardHandler) WithGroup(string) slog.Handler            { return d }
