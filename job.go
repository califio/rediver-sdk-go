package rediver

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

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

// Integration holds third-party integration tokens for the job.
type Integration struct {
	CloudflareTokens []string
}

// Repository contains git repository information for CI/SAST jobs.
// ArtifactID is non-empty when the source code is delivered as a pre-uploaded
// artifact (tar.gz) rather than a live git clone.
type Repository struct {
	URL           string
	Provider      string // "gitlab" | "github" — scanner may behave differently per provider
	Event         string // "push" | "merge_request" | "pull_request"
	Ref           string // raw ref: "refs/heads/main", "refs/tags/v1.0"
	Branch        string
	CommitSHA     string
	BaseBranch    string
	BaseCommitSHA string
	PrNumber      int
	ArtifactID    string `json:"artifact_id"`
	DiffOnly      bool   // true = scan changed files only
	Username      string
	Password      string
}

// DomainTarget contains domain info from the job target.
type DomainTarget struct {
	ID    string
	Value string
	CNAME string
	IPs   []string
}

// IPTarget contains IP address info from the job target.
type IPTarget struct {
	ID    string
	Value string
}

// SubnetTarget contains subnet info from the job target.
type SubnetTarget struct {
	ID    string
	Value string
}

// ServiceTarget contains service endpoint info from the job target.
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

func newJob(detail *agentv1.GetJobDetailResponse) Job {
	j := &job{detail: detail}
	if detail != nil {
		j.resolveParams()
	}
	return j
}

// newCIJob creates a Job from CIContext data. No GetJobDetailResponse needed.
func newCIJob(jobID string, ci *CIContext, scannerName string, params map[string]interface{}) Job {
	refType := agentv1.GitRefType_GIT_REF_TYPE_BRANCH
	switch ci.Ref.Type {
	case CIRefTypeTag:
		refType = agentv1.GitRefType_GIT_REF_TYPE_TAG
	case CIRefTypePRMR:
		refType = agentv1.GitRefType_GIT_REF_TYPE_PR_MR
	}

	// CiEvent: map CI ref type to proto CiEvent
	ciEvent := agentv1.CiEvent_CI_EVENT_PUSH
	if ci.Ref.Type == CIRefTypePRMR {
		ciEvent = agentv1.CiEvent_CI_EVENT_PULL_REQUEST
	}

	repoCtx := &agentv1.RepositoryJobContext{
		Url:    ci.Repo.URL,
		Event:  ciEvent,
		Branch: strOptionalVal(ci.Ref.Branch),
	}
	if ci.Ref.CommitSHA != "" {
		repoCtx.CommitSha = &ci.Ref.CommitSHA
	}
	if ci.Ref.BaseCommitSHA != "" {
		repoCtx.BaseCommitSha = &ci.Ref.BaseCommitSHA
	}
	if ci.Ref.BaseBranch != "" {
		repoCtx.BaseBranch = &ci.Ref.BaseBranch
	}
	// Map GitRefType to GitRefType proto for ref field
	_ = refType // ref type embedded in CiJobGitRef for CreateCiJob; here just use event

	detail := &agentv1.GetJobDetailResponse{
		Id:      jobID,
		Scanner: scannerName,
		Target: &agentv1.JobTarget{
			Repository: repoCtx,
		},
	}
	if len(params) > 0 {
		// params stored separately; GetJobDetailResponse uses google.protobuf.Struct
		// We store params as the plain map and skip setting detail.Params
	}

	j := &job{
		detail:    detail,
		ciContext: ci,
		params:    params,
	}
	return j
}

// resolveParams copies the params from the job detail's Struct into a plain map.
func (j *job) resolveParams() {
	if j.detail == nil || j.detail.Params == nil {
		return
	}
	// google.protobuf.Struct fields are map[string]*structpb.Value
	j.params = make(map[string]interface{})
	for k, v := range j.detail.Params.GetFields() {
		j.params[k] = structValueToInterface(v)
	}
}

// structValueToInterface converts a protobuf Struct value to a Go interface{}.
func structValueToInterface(v interface{ GetStringValue() string; GetNumberValue() float64; GetBoolValue() bool }) interface{} {
	// Use duck-typed interface for structpb.Value methods
	type structVal interface {
		GetStringValue() string
		GetNumberValue() float64
		GetBoolValue() bool
		GetKind() interface{}
	}
	return v
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

func (j *job) Domains() []DomainTarget {
	if j.detail == nil || j.detail.Target == nil {
		return nil
	}
	domains := j.detail.Target.GetDomains()
	result := make([]DomainTarget, 0, len(domains))
	for _, d := range domains {
		dt := DomainTarget{
			Value: d.GetValue(),
			IPs:   d.GetIps(),
		}
		if id := d.GetId(); id != "" {
			dt.ID = id
		}
		if cn := d.GetCname(); cn != "" {
			dt.CNAME = cn
		}
		result = append(result, dt)
	}
	return result
}

func (j *job) IPs() []IPTarget {
	if j.detail == nil || j.detail.Target == nil {
		return nil
	}
	ips := j.detail.Target.GetIps()
	result := make([]IPTarget, 0, len(ips))
	for _, ip := range ips {
		it := IPTarget{Value: ip.GetValue()}
		if id := ip.GetId(); id != "" {
			it.ID = id
		}
		result = append(result, it)
	}
	return result
}

func (j *job) Subnets() []SubnetTarget {
	if j.detail == nil || j.detail.Target == nil {
		return nil
	}
	subnets := j.detail.Target.GetSubnets()
	result := make([]SubnetTarget, 0, len(subnets))
	for _, s := range subnets {
		st := SubnetTarget{Value: s.GetValue()}
		if id := s.GetId(); id != "" {
			st.ID = id
		}
		result = append(result, st)
	}
	return result
}

func (j *job) Services() []ServiceTarget {
	if j.detail == nil || j.detail.Target == nil {
		return nil
	}
	services := j.detail.Target.GetServices()
	result := make([]ServiceTarget, 0, len(services))
	for _, s := range services {
		st := ServiceTarget{
			Value: s.GetValue(),
			Host:  s.GetHost(),
			Port:  int(s.GetPort()),
			URL:   s.GetUrl(),
		}
		if id := s.GetId(); id != "" {
			st.ID = id
		}
		result = append(result, st)
	}
	return result
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
	repo := &Repository{
		URL:           r.GetUrl(),
		Event:         ciEventToString(r.GetEvent()),
		Ref:           r.GetRef(),
		Branch:        r.GetBranch(),
		CommitSHA:     r.GetCommitSha(),
		BaseBranch:    r.GetBaseBranch(),
		BaseCommitSHA: r.GetBaseCommitSha(),
		PrNumber:      int(r.GetPrNumber()),
		ArtifactID:    r.GetArtifactId(),
		DiffOnly:      r.GetDiffOnly(),
	}
	// Map proto GitProvider enum to string
	repo.Provider = gitProviderToString(r.GetProvider())

	if cred := r.GetCredential(); cred != nil {
		repo.Username = cred.GetUsername()
		repo.Password = cred.GetPassword()
	}

	// Populate CommitSHA from resolved HEAD when server didn't provide it
	if repo.CommitSHA == "" && j.resolvedHeadSHA != "" {
		repo.CommitSHA = j.resolvedHeadSHA
	}
	// Populate BaseCommitSHA from resolved merge-base when server didn't provide it
	if repo.BaseCommitSHA == "" && j.resolvedBaseSHA != "" {
		repo.BaseCommitSHA = j.resolvedBaseSHA
	}
	return repo, true
}

// ciEventToString converts a proto CiEvent enum to the string used by scanner logic.
func ciEventToString(e agentv1.CiEvent) string {
	switch e {
	case agentv1.CiEvent_CI_EVENT_PUSH:
		return "push"
	case agentv1.CiEvent_CI_EVENT_PULL_REQUEST:
		return "pull_request"
	case agentv1.CiEvent_CI_EVENT_TAG:
		return "tag"
	default:
		return ""
	}
}

// gitProviderToString converts a proto GitProvider enum to a lowercase provider string.
func gitProviderToString(p agentv1.GitProvider) string {
	switch p {
	case agentv1.GitProvider_GIT_PROVIDER_GITLAB:
		return "gitlab"
	case agentv1.GitProvider_GIT_PROVIDER_GITHUB:
		return "github"
	case agentv1.GitProvider_GIT_PROVIDER_BITBUCKET:
		return "bitbucket"
	default:
		return ""
	}
}

func (j *job) RepoDir() string {
	return j.repoDir
}

// prepareRepository sets up the repo directory for scanning.
func (j *job) prepareRepository(ctx context.Context) error {
	if j.ciContext != nil {
		j.repoDir = j.ciContext.RepoDir
		return nil
	}

	repo, ok := j.Repository()
	if !ok {
		return fmt.Errorf("no repository target")
	}

	// Artifact path: download pre-uploaded tar.gz instead of git clone.
	if repo.ArtifactID != "" {
		return j.prepareArchive(ctx, repo)
	}

	repoURL, err := j.buildRepoURL(repo)
	if err != nil {
		return err
	}

	workDir, err := os.MkdirTemp(os.TempDir(), "repo_")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}

	refs, checkoutRef := buildRefSpecs(repo)
	err = utils.GitCheckout(ctx, utils.CheckoutOptions{
		WorkDir:     workDir,
		RepoURL:     repoURL,
		Refs:        refs,
		CheckoutRef: checkoutRef,
	})
	if err != nil {
		os.RemoveAll(workDir)
		return fmt.Errorf("checkout: %w", err)
	}

	j.repoDir = workDir
	j.clonedRepoDir = workDir

	if repo.BaseCommitSHA != "" {
		utils.EnsureMergeBaseReachable(ctx, workDir, repo.BaseCommitSHA)
	}

	if repo.CommitSHA == "" {
		if sha, err := utils.GitRevParseHead(ctx, workDir); err == nil && sha != "" {
			j.resolvedHeadSHA = sha
		}
	}

	if (repo.Event == "merge_request" || repo.Event == "pull_request") && repo.BaseBranch != "" {
		if sha, err := utils.GitMergeBase(ctx, workDir, "origin/"+repo.BaseBranch, "HEAD"); err == nil && sha != "" {
			j.resolvedBaseSHA = sha
		}
	}

	return nil
}

// buildRefSpecs constructs git refspecs and checkout ref based on event type and provider.
func buildRefSpecs(repo *Repository) (refs []string, checkoutRef string) {
	switch repo.Event {
	case "merge_request", "pull_request":
		refs, checkoutRef = buildMrPrRefSpecs(repo)
	default:
		if strings.HasPrefix(repo.Ref, "refs/tags/") {
			refs = append(refs, repo.Ref)
		} else if repo.Branch != "" {
			refs = append(refs, fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", repo.Branch, repo.Branch))
		}
		if repo.BaseCommitSHA != "" {
			refs = append(refs, repo.BaseCommitSHA)
		}
		checkoutRef = repo.CommitSHA
	}

	if len(refs) == 0 {
		if repo.CommitSHA != "" {
			refs = append(refs, repo.CommitSHA)
		} else if repo.Branch != "" {
			refs = append(refs, fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", repo.Branch, repo.Branch))
		}
	}
	if checkoutRef == "" {
		if repo.CommitSHA != "" {
			checkoutRef = repo.CommitSHA
		} else if repo.Branch != "" {
			checkoutRef = "origin/" + repo.Branch
		}
	}

	return refs, checkoutRef
}

// buildMrPrRefSpecs returns fetch refs + checkout ref for MR/PR events.
func buildMrPrRefSpecs(repo *Repository) (refs []string, checkoutRef string) {
	switch repo.Provider {
	case "gitlab":
		if repo.PrNumber > 0 {
			refs = append(refs, fmt.Sprintf("refs/merge-requests/%d/head", repo.PrNumber))
		}
		checkoutRef = "FETCH_HEAD"
	case "github":
		if repo.CommitSHA != "" {
			refs = append(refs, repo.CommitSHA)
		}
		checkoutRef = repo.CommitSHA
	case "bitbucket":
		if repo.PrNumber > 0 {
			refs = append(refs, fmt.Sprintf("refs/pull-requests/%d/from", repo.PrNumber))
		}
		checkoutRef = "FETCH_HEAD"
	default:
		if repo.CommitSHA != "" {
			refs = append(refs, repo.CommitSHA)
		}
		checkoutRef = repo.CommitSHA
	}

	if repo.BaseBranch != "" {
		refs = append(refs, fmt.Sprintf("refs/heads/%s", repo.BaseBranch))
	}

	return refs, checkoutRef
}

// prepareArchive downloads the artifact presigned URL and extracts the tar.gz
// into a temp directory, then sets j.repoDir to the extracted path.
func (j *job) prepareArchive(ctx context.Context, repo *Repository) error {
	if j.artifactDownloadFn == nil {
		return fmt.Errorf("artifact download not available in this run mode")
	}

	presignedURL, err := j.artifactDownloadFn(ctx, repo.ArtifactID)
	if err != nil {
		return fmt.Errorf("get artifact download URL: %w", err)
	}

	tmpDir, err := os.MkdirTemp(os.TempDir(), "artifact_")
	if err != nil {
		return fmt.Errorf("create artifact temp dir: %w", err)
	}

	if err := j.downloadAndExtract(ctx, presignedURL, tmpDir); err != nil {
		os.RemoveAll(tmpDir)
		return fmt.Errorf("extract artifact: %w", err)
	}

	j.repoDir = tmpDir
	j.clonedRepoDir = tmpDir
	return nil
}

// downloadAndExtract streams a tar.gz from url and extracts it into destDir.
func (j *job) downloadAndExtract(ctx context.Context, rawURL string, destDir string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("build download request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download artifact: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("download artifact: unexpected status %d", resp.StatusCode)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("open gzip stream: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		cleanRel := filepath.Clean("/" + hdr.Name)
		target := filepath.Join(destDir, cleanRel)
		if !strings.HasPrefix(target+string(filepath.Separator), destDir+string(filepath.Separator)) {
			return fmt.Errorf("tar entry %q escapes destination directory", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0750); err != nil {
				return fmt.Errorf("create dir %q: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0750); err != nil {
				return fmt.Errorf("create parent dir for %q: %w", target, err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0750)
			if err != nil {
				return fmt.Errorf("create file %q: %w", target, err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("write file %q: %w", target, err)
			}
			f.Close()
		}
	}
	return nil
}

// cleanupRepository removes the cloned repo directory if the SDK created it.
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

	if repo.CommitSHA == "" {
		return nil, nil
	}

	if j.repoDir == "" {
		return nil, fmt.Errorf("repository not prepared")
	}

	baseRef := repo.BaseCommitSHA
	if baseRef == "" && repo.BaseBranch != "" {
		baseRef = "origin/" + repo.BaseBranch
	}
	if baseRef == "" {
		return nil, nil
	}

	return utils.GitDiff(ctx, j.repoDir, baseRef, "HEAD")
}

func (j *job) Integration() *Integration {
	if j.detail == nil || j.detail.Integration == nil {
		return nil
	}
	tokens := j.detail.Integration.GetCloudflareTokens()
	if len(tokens) == 0 {
		return nil
	}
	return &Integration{CloudflareTokens: tokens}
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
