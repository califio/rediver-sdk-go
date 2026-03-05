package rediver

import (
	"github.com/califio/rediver-sdk-go/internal/api"
)

// GitProvider is a type alias for the generated API GitProvider enum.
type GitProvider = api.GitProvider

const (
	GitProviderGitHub    = api.GitProviderGitHub
	GitProviderGitLab    = api.GitProviderGitLab
	GitProviderBitbucket = api.GitProviderBitbucket
	GitProviderUnknown   = api.GitProviderUnknown
)

// CIContext holds all CI environment information needed to create and execute a CI job.
// Use DetectGitContext() to auto-populate from environment variables, or construct manually.
type CIContext struct {
	// Local environment
	RepoDir string // local checkout directory (e.g., $CI_PROJECT_DIR)

	// CI pipeline
	JobURL  string // CI job URL
	JobName string // CI job name

	// Scan source: "gitlab-ci", "github-action", "local"
	Source string

	// Git provider
	Provider GitProvider

	// Repository
	Repo CIRepo

	// Git ref
	Ref CIRef

	// Scanner parameters (optional overrides, lower priority than env vars)
	Parameters map[string]interface{}
}

// CIRepo contains repository metadata from the CI environment.
type CIRepo struct {
	ID            string // provider-native repo ID
	Name          string // "org/repo-name"
	URL           string // clone URL
	HtmlURL       string // web URL
	DefaultBranch string
	Private       bool
}

// CIRef contains git ref information from the CI environment.
type CIRef struct {
	Type CIRefType
	Name string

	CommitSHA     string
	BaseCommitSHA string
	CommitMessage string

	Branch      string
	BaseBranch  string
	IsDefault   bool
	IsProtected bool

	// PR/MR metadata
	PRNumber string
	PRTitle  string
	PRURL    string
	PRAuthor string
	PRAction string // "opened", "synchronize", "reopened", "merged"
	PRLabels []string
	PRDraft  bool
}

// CIRefType indicates the type of git reference.
type CIRefType string

const (
	CIRefTypeBranch CIRefType = "branch"
	CIRefTypeTag    CIRefType = "tag"
	CIRefTypePRMR   CIRefType = "pr_mr"
)
