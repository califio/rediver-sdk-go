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

// artifactDownloadFunc fetches a presigned download URL for the given artifactID.
// Returns the presigned URL string on success.
type artifactDownloadFunc func(ctx context.Context, artifactID string) (string, error)

// job is the internal implementation of Job.
type job struct {
	detail               *api.JobDetail
	params               map[string]interface{}
	ciContext            *CIContext            // non-nil = CI mode
	logger               *slog.Logger          // job-scoped logger (multi-handler: console + buffer)
	repoDir              string                // prepared repo path (CI dir or cloned temp dir)
	clonedRepoDir        string                // non-empty only when SDK cloned it → needs cleanup
	resolvedBaseSHA      string                // resolved via git merge-base when server didn't provide BaseCommitSHA
	resolvedHeadSHA      string                // resolved via git rev-parse HEAD when server-provided CommitSHA is empty
	artifactDownloadFn   artifactDownloadFunc  // injected by Agent for artifact-based repos
}

func newJob(detail *api.JobDetail) Job {
	j := &job{detail: detail}
	if detail != nil {
		j.resolveParams()
	}
	return j
}

// newCIJob creates a Job from CIContext data. No api.JobDetail needed.
// The job ID comes from the create-job API response.
func newCIJob(jobID string, ci *CIContext, scannerName string, params map[string]interface{}) Job {
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
		Repository: &api.RepositoryJobContext{
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
		detail:    detail,
		ciContext: ci,
		params:    params,
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
	if r.Provider != nil {
		repo.Provider = *r.Provider
	}
	if r.Event != nil {
		repo.Event = *r.Event
	}
	if r.Ref != nil {
		repo.Ref = *r.Ref
	}
	if r.Branch != nil {
		repo.Branch = *r.Branch
	}
	if r.CommitSha != nil {
		repo.CommitSHA = *r.CommitSha
	}
	if r.BaseBranch != nil {
		repo.BaseBranch = *r.BaseBranch
	}
	if r.BaseCommitSha != nil {
		repo.BaseCommitSHA = *r.BaseCommitSha
	}
	if r.PrNumber != nil {
		repo.PrNumber = int(*r.PrNumber)
	}
	if r.ArtifactId != nil {
		repo.ArtifactID = *r.ArtifactId
	}
	if r.DiffOnly != nil {
		repo.DiffOnly = *r.DiffOnly
	}
	if r.Credential != nil {
		if r.Credential.Username != nil {
			repo.Username = *r.Credential.Username
		}
		if r.Credential.Password != nil {
			repo.Password = *r.Credential.Password
		}
	}
	// Populate CommitSHA from resolved HEAD if server didn't provide it
	if repo.CommitSHA == "" && j.resolvedHeadSHA != "" {
		repo.CommitSHA = j.resolvedHeadSHA
	}
	// Populate BaseCommitSHA from resolved merge-base if server didn't provide it
	if repo.BaseCommitSHA == "" && j.resolvedBaseSHA != "" {
		repo.BaseCommitSHA = j.resolvedBaseSHA
	}
	return repo, true
}

func (j *job) RepoDir() string {
	return j.repoDir
}

// prepareRepository sets up the repo directory for scanning.
// CI mode: uses CI-provided checkout dir.
// Artifact mode: downloads and extracts the pre-uploaded tar.gz artifact.
// Worker mode: clones to temp dir via git.
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
	// Connector already resolved merge-base and included .git in archive.
	// BaseCommitSHA from job context is the resolved merge-base — use directly.
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

	// Build refspecs based on event type and provider.
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
	j.clonedRepoDir = workDir // track for cleanup

	// Shallow clones (--depth=1) may not connect HEAD to BaseCommitSHA in the
	// commit graph. Deepen progressively until merge-base is reachable so that
	// scanners can compute diffs. Caps at 200 extra commits to avoid fetching
	// full history on large repos — if still unreachable, scanners fall back to
	// full scan.
	if repo.BaseCommitSHA != "" {
		utils.EnsureMergeBaseReachable(ctx, workDir, repo.BaseCommitSHA)
	}

	// Resolve actual HEAD SHA — needed when server-provided CommitSHA is empty
	// (e.g., manual trigger on connector-synced repo without commit info).
	if repo.CommitSHA == "" {
		if sha, err := utils.GitRevParseHead(ctx, workDir); err == nil && sha != "" {
			j.resolvedHeadSHA = sha
		}
	}

	// For MR/PR: always resolve base commit from target branch via merge-base.
	// More reliable than server-provided BaseCommitSHA (oldrev) because:
	// 1. MR create: server sends empty BaseCommitSHA
	// 2. MR update: server sends oldrev (previous head) which may not be fetchable in shallow clone
	// 3. merge-base finds the actual common ancestor — correct for diff
	if (repo.Event == "merge_request" || repo.Event == "pull_request") && repo.BaseBranch != "" {
		if sha, err := utils.GitMergeBase(ctx, workDir, "origin/"+repo.BaseBranch, "HEAD"); err == nil && sha != "" {
			j.resolvedBaseSHA = sha
		}
	}

	return nil
}

// buildRefSpecs constructs git refspecs and checkout ref based on event type and provider.
//
// Strategy per event:
//   - push:           fetch branch ref, checkout commit SHA
//   - merge_request:  fetch MR head ref + base branch, checkout MR head (for diff: base..head)
//   - pull_request:   fetch PR head SHA + base SHA (GitHub allows fetch by SHA), checkout head
//
// Returns (refspecs to fetch, ref to checkout after fetch).
func buildRefSpecs(repo *Repository) (refs []string, checkoutRef string) {
	switch repo.Event {
	case "merge_request", "pull_request":
		refs, checkoutRef = buildMrPrRefSpecs(repo)
	default:
		// Push/tag: detect tag vs branch from Ref field
		if strings.HasPrefix(repo.Ref, "refs/tags/") {
			refs = append(refs, repo.Ref)
		} else if repo.Branch != "" {
			refs = append(refs, fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", repo.Branch, repo.Branch))
		}
		// Fetch base commit for diff (push "before" SHA)
		if repo.BaseCommitSHA != "" {
			refs = append(refs, repo.BaseCommitSHA)
		}
		checkoutRef = repo.CommitSHA
	}

	// Fallback: if no refs collected, try CommitSHA then branch
	if len(refs) == 0 {
		if repo.CommitSHA != "" {
			refs = append(refs, repo.CommitSHA)
		} else if repo.Branch != "" {
			refs = append(refs, fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", repo.Branch, repo.Branch))
		}
	}
	// Fallback checkout: try CommitSHA, then branch remote ref
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
// Each provider uses different ref namespaces for merge requests.
// Always fetches both head + base for diff capability (git diff base..head).
func buildMrPrRefSpecs(repo *Repository) (refs []string, checkoutRef string) {
	// Head ref — provider-specific
	switch repo.Provider {
	case "gitlab":
		// GitLab: must use named ref (doesn't allow fetch by SHA)
		if repo.PrNumber > 0 {
			refs = append(refs, fmt.Sprintf("refs/merge-requests/%d/head", repo.PrNumber))
		}
		checkoutRef = "FETCH_HEAD"

	case "github":
		// GitHub: allows fetch by SHA directly
		if repo.CommitSHA != "" {
			refs = append(refs, repo.CommitSHA)
		}
		checkoutRef = repo.CommitSHA

	case "bitbucket":
		// Bitbucket: uses refs/pull-requests/{id}/from
		if repo.PrNumber > 0 {
			refs = append(refs, fmt.Sprintf("refs/pull-requests/%d/from", repo.PrNumber))
		}
		checkoutRef = "FETCH_HEAD"

	default:
		// Unknown provider: try SHA, fallback to branch
		if repo.CommitSHA != "" {
			refs = append(refs, repo.CommitSHA)
		}
		checkoutRef = repo.CommitSHA
	}

	// Base ref — always fetch base branch for merge-base resolution
	// Don't fetch BaseCommitSHA (oldrev) — may not be fetchable in shallow clone
	// SDK resolves actual base via git merge-base after checkout
	if repo.BaseBranch != "" {
		refs = append(refs, fmt.Sprintf("refs/heads/%s", repo.BaseBranch))
	}

	return refs, checkoutRef
}

// prepareArchive downloads the artifact presigned URL and extracts the tar.gz
// into a temp directory, then sets j.repoDir to the extracted path.
// Path traversal is prevented by rejecting any entry whose resolved path
// does not have the temp dir as a prefix.
func (j *job) prepareArchive(ctx context.Context, repo *Repository) error {
	if j.artifactDownloadFn == nil {
		return fmt.Errorf("artifact download not available in this run mode")
	}

	// 1. Get presigned download URL from backend.
	presignedURL, err := j.artifactDownloadFn(ctx, repo.ArtifactID)
	if err != nil {
		return fmt.Errorf("get artifact download URL: %w", err)
	}

	// 2. Prepare extraction directory.
	tmpDir, err := os.MkdirTemp(os.TempDir(), "artifact_")
	if err != nil {
		return fmt.Errorf("create artifact temp dir: %w", err)
	}

	// 3. Stream-download and extract — no full-file buffering.
	if err := j.downloadAndExtract(ctx, presignedURL, tmpDir); err != nil {
		os.RemoveAll(tmpDir)
		return fmt.Errorf("extract artifact: %w", err)
	}

	j.repoDir = tmpDir
	j.clonedRepoDir = tmpDir // track for cleanup
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

		// Path traversal guard: resolve against destDir and verify prefix.
		cleanRel := filepath.Clean("/" + hdr.Name) // collapse ".." sequences
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
		// Symlinks and other special types are skipped for security.
	}
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

	if repo.CommitSHA == "" {
		return nil, nil
	}

	if j.repoDir == "" {
		return nil, fmt.Errorf("repository not prepared")
	}

	// Resolve base ref for diff:
	// 1. BaseCommitSHA if available (MR update events provide this)
	// 2. Fallback to origin/{base_branch} (MR create events — base branch was fetched by buildRefSpecs)
	// 3. No base available → return nil (full scan, no diff)
	baseRef := repo.BaseCommitSHA
	if baseRef == "" && repo.BaseBranch != "" {
		baseRef = "origin/" + repo.BaseBranch
	}
	if baseRef == "" {
		return nil, nil
	}

	// Use HEAD (current checkout) as head ref — more reliable than CommitSHA
	// because GitLab MR checkout lands on FETCH_HEAD, not a named ref.
	return utils.GitDiff(ctx, j.repoDir, baseRef, "HEAD")
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
