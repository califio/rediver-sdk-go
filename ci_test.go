package rediver

import (
	"context"
	"os"
	"testing"
)

func TestDetectGitContext_GitLab(t *testing.T) {
	// Set GitLab CI env vars
	envs := map[string]string{
		"GITLAB_CI":                           "true",
		"CI_PROJECT_DIR":                      "/builds/org/repo",
		"CI_JOB_URL":                          "https://gitlab.com/org/repo/-/jobs/123",
		"CI_JOB_NAME":                         "scan",
		"CI_PROJECT_ID":                       "42",
		"CI_PROJECT_PATH":                     "org/repo",
		"CI_REPOSITORY_URL":                   "https://gitlab.com/org/repo.git",
		"CI_PROJECT_URL":                      "https://gitlab.com/org/repo",
		"CI_DEFAULT_BRANCH":                   "main",
		"CI_COMMIT_SHA":                       "abc123def456",
		"CI_COMMIT_BRANCH":                    "feature-branch",
		"CI_COMMIT_MESSAGE":                   "Add new feature",
		"CI_COMMIT_REF_PROTECTED":             "false",
	}
	for k, v := range envs {
		t.Setenv(k, v)
	}

	ci := DetectGitContext()
	if ci == nil {
		t.Fatal("expected CIContext, got nil")
	}

	if ci.Source != "gitlab-ci" {
		t.Errorf("expected source gitlab-ci, got %s", ci.Source)
	}
	if ci.Provider != GitProviderGitLab {
		t.Errorf("expected GitLab, got %s", ci.Provider)
	}
	if ci.RepoDir != "/builds/org/repo" {
		t.Errorf("expected /builds/org/repo, got %s", ci.RepoDir)
	}
	if ci.Repo.Name != "org/repo" {
		t.Errorf("expected org/repo, got %s", ci.Repo.Name)
	}
	if ci.Ref.Type != CIRefTypeBranch {
		t.Errorf("expected branch, got %s", ci.Ref.Type)
	}
	if ci.Ref.CommitSHA != "abc123def456" {
		t.Errorf("expected abc123def456, got %s", ci.Ref.CommitSHA)
	}
	if ci.Ref.Branch != "feature-branch" {
		t.Errorf("expected feature-branch, got %s", ci.Ref.Branch)
	}
}

func TestDetectGitContext_GitLabMR(t *testing.T) {
	envs := map[string]string{
		"GITLAB_CI":                              "true",
		"CI_PROJECT_DIR":                         "/builds/org/repo",
		"CI_JOB_URL":                             "https://gitlab.com/org/repo/-/jobs/123",
		"CI_JOB_NAME":                            "scan",
		"CI_PROJECT_ID":                          "42",
		"CI_PROJECT_PATH":                        "org/repo",
		"CI_REPOSITORY_URL":                      "https://gitlab.com/org/repo.git",
		"CI_PROJECT_URL":                         "https://gitlab.com/org/repo",
		"CI_DEFAULT_BRANCH":                      "main",
		"CI_COMMIT_SHA":                          "abc123",
		"CI_MERGE_REQUEST_IID":                   "99",
		"CI_MERGE_REQUEST_TITLE":                 "Fix bug",
		"CI_MERGE_REQUEST_TARGET_BRANCH_NAME":    "main",
		"CI_MERGE_REQUEST_DIFF_BASE_SHA":         "base123",
		"CI_MERGE_REQUEST_SOURCE_BRANCH_NAME":    "fix-branch",
		"CI_MERGE_REQUEST_AUTHOR":                "dev",
		"CI_MERGE_REQUEST_LABELS":                "bug,priority",
	}
	for k, v := range envs {
		t.Setenv(k, v)
	}

	ci := DetectGitContext()
	if ci == nil {
		t.Fatal("expected CIContext, got nil")
	}

	if ci.Ref.Type != CIRefTypePRMR {
		t.Errorf("expected pr_mr, got %s", ci.Ref.Type)
	}
	if ci.Ref.PRNumber != "99" {
		t.Errorf("expected 99, got %s", ci.Ref.PRNumber)
	}
	if ci.Ref.PRTitle != "Fix bug" {
		t.Errorf("expected Fix bug, got %s", ci.Ref.PRTitle)
	}
	if ci.Ref.BaseCommitSHA != "base123" {
		t.Errorf("expected base123, got %s", ci.Ref.BaseCommitSHA)
	}
	if len(ci.Ref.PRLabels) != 2 || ci.Ref.PRLabels[0] != "bug" {
		t.Errorf("expected [bug, priority], got %v", ci.Ref.PRLabels)
	}
}

func TestDetectGitContext_GitHub(t *testing.T) {
	envs := map[string]string{
		"GITHUB_ACTIONS":    "true",
		"GITHUB_WORKSPACE":  "/home/runner/work/repo/repo",
		"GITHUB_SERVER_URL": "https://github.com",
		"GITHUB_REPOSITORY": "org/repo",
		"GITHUB_RUN_ID":     "456",
		"GITHUB_JOB":        "scan",
		"GITHUB_SHA":        "sha789",
		"GITHUB_REF":        "refs/heads/main",
		"GITHUB_REF_NAME":   "main",
	}
	for k, v := range envs {
		t.Setenv(k, v)
	}

	ci := DetectGitContext()
	if ci == nil {
		t.Fatal("expected CIContext, got nil")
	}

	if ci.Source != "github-action" {
		t.Errorf("expected source github-action, got %s", ci.Source)
	}
	if ci.Provider != GitProviderGitHub {
		t.Errorf("expected GitHub, got %s", ci.Provider)
	}
	if ci.RepoDir != "/home/runner/work/repo/repo" {
		t.Errorf("expected workspace, got %s", ci.RepoDir)
	}
	if ci.Ref.Type != CIRefTypeBranch {
		t.Errorf("expected branch, got %s", ci.Ref.Type)
	}
	if ci.Ref.Branch != "main" {
		t.Errorf("expected main, got %s", ci.Ref.Branch)
	}
}

func TestDetectGitContext_GitHubPR(t *testing.T) {
	envs := map[string]string{
		"GITHUB_ACTIONS":    "true",
		"GITHUB_WORKSPACE":  "/home/runner/work/repo/repo",
		"GITHUB_SERVER_URL": "https://github.com",
		"GITHUB_REPOSITORY": "org/repo",
		"GITHUB_RUN_ID":     "456",
		"GITHUB_JOB":        "scan",
		"GITHUB_SHA":        "sha789",
		"GITHUB_REF":        "refs/pull/42/merge",
		"GITHUB_REF_NAME":   "42/merge",
		"GITHUB_BASE_REF":   "main",
		"GITHUB_HEAD_REF":   "feature",
		"GITHUB_EVENT_NAME": "pull_request",
	}
	for k, v := range envs {
		t.Setenv(k, v)
	}

	ci := DetectGitContext()
	if ci == nil {
		t.Fatal("expected CIContext, got nil")
	}

	if ci.Ref.Type != CIRefTypePRMR {
		t.Errorf("expected pr_mr, got %s", ci.Ref.Type)
	}
	if ci.Ref.PRNumber != "42" {
		t.Errorf("expected 42, got %s", ci.Ref.PRNumber)
	}
	if ci.Ref.BaseBranch != "main" {
		t.Errorf("expected main, got %s", ci.Ref.BaseBranch)
	}
}

func TestDetectGitContext_LocalFallback(t *testing.T) {
	// Ensure no CI env vars are set
	os.Unsetenv("GITLAB_CI")
	os.Unsetenv("GITHUB_ACTIONS")

	ci := DetectGitContext()
	// When run inside a git repo (like this test suite), should detect local git
	if ci == nil {
		t.Skip("not inside a git repository, skipping local fallback test")
	}
	if ci.Source != "local" {
		t.Errorf("expected source 'local', got %s", ci.Source)
	}
	if ci.Ref.CommitSHA == "" {
		t.Error("expected non-empty commit SHA")
	}
}

func TestPrepareRepository_CIMode(t *testing.T) {
	ci := &CIContext{
		RepoDir: "/builds/org/repo",
	}
	j := &job{ciContext: ci}

	err := j.prepareRepository(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if j.RepoDir() != "/builds/org/repo" {
		t.Errorf("expected /builds/org/repo, got %s", j.RepoDir())
	}
	// CI mode should not set clonedRepoDir (no cleanup needed)
	if j.clonedRepoDir != "" {
		t.Errorf("expected empty clonedRepoDir in CI mode, got %s", j.clonedRepoDir)
	}
}

func TestResolveParamsFromEnv(t *testing.T) {
	// Set env var
	t.Setenv("SEMGREP_RULES", "p/security")

	params := []Param{
		StringParam("rules").Env("SEMGREP_RULES").Default("p/default").Build(),
		StringParam("output").Default("json").Build(),
		StringParam("config").Env("SEMGREP_CONFIG").Build(), // env not set, no default
	}

	resolved := resolveParamsFromEnv(params, nil)

	if resolved["rules"] != "p/security" {
		t.Errorf("expected p/security from env, got %v", resolved["rules"])
	}
	if resolved["output"] != "json" {
		t.Errorf("expected json from default, got %v", resolved["output"])
	}
	if _, ok := resolved["config"]; ok {
		t.Error("expected config to not be set")
	}
}

func TestResolveParamsFromEnv_CIParamsOverride(t *testing.T) {
	params := []Param{
		StringParam("rules").Env("SEMGREP_RULES_TEST").Default("p/default").Build(),
	}

	ciParams := map[string]interface{}{
		"rules": "p/custom",
	}

	// No env var set
	os.Unsetenv("SEMGREP_RULES_TEST")

	resolved := resolveParamsFromEnv(params, ciParams)
	if resolved["rules"] != "p/custom" {
		t.Errorf("expected p/custom from ciParams, got %v", resolved["rules"])
	}
}
