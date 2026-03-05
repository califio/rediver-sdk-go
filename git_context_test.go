package rediver

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// --- Live detection: run on actual machine and print detected context ---

func TestDetectGitContext_LiveReport(t *testing.T) {
	// Clear CI env vars to ensure local detection
	os.Unsetenv("GITLAB_CI")
	os.Unsetenv("GITHUB_ACTIONS")

	ci := DetectGitContext()
	if ci == nil {
		t.Skip("not inside a git repository")
	}

	// Print detected context for manual inspection
	fmt.Println("=== Detected Git Context ===")
	fmt.Printf("  Source:         %s\n", ci.Source)
	fmt.Printf("  Provider:       %s\n", ci.Provider)
	fmt.Printf("  RepoDir:        %s\n", ci.RepoDir)
	fmt.Printf("  Repo.ID:        %s\n", ci.Repo.ID)
	fmt.Printf("  Repo.Name:      %s\n", ci.Repo.Name)
	fmt.Printf("  Repo.URL:       %s\n", ci.Repo.URL)
	fmt.Printf("  Repo.HtmlURL:   %s\n", ci.Repo.HtmlURL)
	fmt.Printf("  Repo.Default:   %s\n", ci.Repo.DefaultBranch)
	fmt.Printf("  Ref.Type:       %s\n", ci.Ref.Type)
	fmt.Printf("  Ref.Name:       %s\n", ci.Ref.Name)
	fmt.Printf("  Ref.Branch:     %s\n", ci.Ref.Branch)
	fmt.Printf("  Ref.CommitSHA:  %s\n", ci.Ref.CommitSHA)
	fmt.Printf("  Ref.IsDefault:  %v\n", ci.Ref.IsDefault)
	fmt.Println("============================")

	// Validate essential fields are populated
	if ci.Source != "local" {
		t.Errorf("expected source 'local', got %s", ci.Source)
	}
	if ci.RepoDir == "" {
		t.Error("RepoDir should not be empty")
	}
	if ci.Ref.CommitSHA == "" {
		t.Error("CommitSHA should not be empty")
	}
	if ci.Ref.Type != CIRefTypeBranch {
		t.Errorf("expected branch ref type for local, got %s", ci.Ref.Type)
	}
	if ci.Repo.Name == "" {
		t.Error("Repo.Name should not be empty")
	}
	// Provider should be detected from remote URL
	if ci.Repo.URL != "" && ci.Provider == GitProviderUnknown {
		t.Logf("warning: remote URL exists (%s) but provider is Unknown", ci.Repo.URL)
	}
}

// --- GitLab CI: Tag push ---

func TestDetectGitContext_GitLabTag(t *testing.T) {
	envs := map[string]string{
		"GITLAB_CI":               "true",
		"CI_PROJECT_DIR":          "/builds/org/repo",
		"CI_JOB_URL":              "https://gitlab.com/org/repo/-/jobs/200",
		"CI_JOB_NAME":             "release-scan",
		"CI_PROJECT_ID":           "42",
		"CI_PROJECT_PATH":         "org/repo",
		"CI_REPOSITORY_URL":       "https://gitlab.com/org/repo.git",
		"CI_PROJECT_URL":          "https://gitlab.com/org/repo",
		"CI_DEFAULT_BRANCH":       "main",
		"CI_COMMIT_SHA":           "tag-sha-abc",
		"CI_COMMIT_TAG":           "v1.2.0",
		"CI_COMMIT_REF_PROTECTED": "true",
	}
	for k, v := range envs {
		t.Setenv(k, v)
	}

	ci := DetectGitContext()
	if ci == nil {
		t.Fatal("expected CIContext, got nil")
	}
	if ci.Ref.Type != CIRefTypeTag {
		t.Errorf("expected tag, got %s", ci.Ref.Type)
	}
	if ci.Ref.Name != "v1.2.0" {
		t.Errorf("expected v1.2.0, got %s", ci.Ref.Name)
	}
	if ci.Ref.CommitSHA != "tag-sha-abc" {
		t.Errorf("expected tag-sha-abc, got %s", ci.Ref.CommitSHA)
	}
	if !ci.Ref.IsProtected {
		t.Error("expected IsProtected true")
	}
}

// --- GitLab CI: Default branch detection ---

func TestDetectGitContext_GitLabDefaultBranch(t *testing.T) {
	envs := map[string]string{
		"GITLAB_CI":               "true",
		"CI_PROJECT_DIR":          "/builds/org/repo",
		"CI_JOB_URL":              "https://gitlab.com/org/repo/-/jobs/300",
		"CI_JOB_NAME":             "scan",
		"CI_PROJECT_ID":           "42",
		"CI_PROJECT_PATH":         "org/repo",
		"CI_REPOSITORY_URL":       "https://gitlab.com/org/repo.git",
		"CI_PROJECT_URL":          "https://gitlab.com/org/repo",
		"CI_DEFAULT_BRANCH":       "main",
		"CI_COMMIT_SHA":           "abc",
		"CI_COMMIT_BRANCH":        "main",
		"CI_COMMIT_REF_PROTECTED": "true",
	}
	for k, v := range envs {
		t.Setenv(k, v)
	}

	ci := DetectGitContext()
	if ci == nil {
		t.Fatal("expected CIContext, got nil")
	}
	if !ci.Ref.IsDefault {
		t.Error("expected IsDefault true when branch == default branch")
	}
	if ci.Ref.Branch != "main" {
		t.Errorf("expected main, got %s", ci.Ref.Branch)
	}
}

// --- GitHub Actions: Tag push ---

func TestDetectGitContext_GitHubTag(t *testing.T) {
	envs := map[string]string{
		"GITHUB_ACTIONS":    "true",
		"GITHUB_WORKSPACE":  "/home/runner/work/repo/repo",
		"GITHUB_SERVER_URL": "https://github.com",
		"GITHUB_REPOSITORY": "org/repo",
		"GITHUB_RUN_ID":     "789",
		"GITHUB_JOB":        "release",
		"GITHUB_SHA":        "tag-sha-999",
		"GITHUB_REF":        "refs/tags/v2.0.0",
		"GITHUB_REF_NAME":   "v2.0.0",
	}
	for k, v := range envs {
		t.Setenv(k, v)
	}

	ci := DetectGitContext()
	if ci == nil {
		t.Fatal("expected CIContext, got nil")
	}
	if ci.Ref.Type != CIRefTypeTag {
		t.Errorf("expected tag, got %s", ci.Ref.Type)
	}
	if ci.Ref.Name != "v2.0.0" {
		t.Errorf("expected v2.0.0, got %s", ci.Ref.Name)
	}
}

// --- GitHub Actions: PR with full event payload ---

func TestDetectGitContext_GitHubPR_EventPayload(t *testing.T) {
	// Create a temp event payload JSON
	payload := map[string]interface{}{
		"action": "opened",
		"pull_request": map[string]interface{}{
			"number": float64(55),
			"title":  "Add authentication",
			"draft":  true,
			"user": map[string]interface{}{
				"login": "contributor",
			},
			"labels": []interface{}{
				map[string]interface{}{"name": "feature"},
				map[string]interface{}{"name": "security"},
			},
			"base": map[string]interface{}{
				"sha": "base-sha-111",
				"ref": "develop",
			},
			"head": map[string]interface{}{
				"sha": "head-sha-222",
				"ref": "feat/auth",
			},
		},
		"repository": map[string]interface{}{
			"id":             float64(12345),
			"full_name":      "myorg/myrepo",
			"html_url":       "https://github.com/myorg/myrepo",
			"clone_url":      "https://github.com/myorg/myrepo.git",
			"default_branch": "develop",
			"private":        true,
		},
	}

	eventFile := filepath.Join(t.TempDir(), "event.json")
	data, _ := json.Marshal(payload)
	if err := os.WriteFile(eventFile, data, 0644); err != nil {
		t.Fatal(err)
	}

	envs := map[string]string{
		"GITHUB_ACTIONS":    "true",
		"GITHUB_WORKSPACE":  "/home/runner/work/myrepo/myrepo",
		"GITHUB_SERVER_URL": "https://github.com",
		"GITHUB_REPOSITORY": "myorg/myrepo",
		"GITHUB_RUN_ID":     "500",
		"GITHUB_JOB":        "scan",
		"GITHUB_SHA":        "merge-sha",
		"GITHUB_REF":        "refs/pull/55/merge",
		"GITHUB_REF_NAME":   "55/merge",
		"GITHUB_EVENT_PATH": eventFile,
		"GITHUB_EVENT_NAME": "pull_request",
	}
	for k, v := range envs {
		t.Setenv(k, v)
	}

	ci := DetectGitContext()
	if ci == nil {
		t.Fatal("expected CIContext, got nil")
	}

	// PR metadata from event payload
	if ci.Ref.Type != CIRefTypePRMR {
		t.Errorf("expected pr_mr, got %s", ci.Ref.Type)
	}
	if ci.Ref.PRNumber != "55" {
		t.Errorf("expected 55, got %s", ci.Ref.PRNumber)
	}
	if ci.Ref.PRTitle != "Add authentication" {
		t.Errorf("expected 'Add authentication', got %s", ci.Ref.PRTitle)
	}
	if !ci.Ref.PRDraft {
		t.Error("expected PRDraft true")
	}
	if ci.Ref.PRAuthor != "contributor" {
		t.Errorf("expected contributor, got %s", ci.Ref.PRAuthor)
	}
	if ci.Ref.PRAction != "opened" {
		t.Errorf("expected opened, got %s", ci.Ref.PRAction)
	}
	if len(ci.Ref.PRLabels) != 2 {
		t.Fatalf("expected 2 labels, got %d", len(ci.Ref.PRLabels))
	}
	if ci.Ref.PRLabels[0] != "feature" || ci.Ref.PRLabels[1] != "security" {
		t.Errorf("expected [feature, security], got %v", ci.Ref.PRLabels)
	}

	// Base/head from event payload
	if ci.Ref.BaseCommitSHA != "base-sha-111" {
		t.Errorf("expected base-sha-111, got %s", ci.Ref.BaseCommitSHA)
	}
	if ci.Ref.BaseBranch != "develop" {
		t.Errorf("expected develop, got %s", ci.Ref.BaseBranch)
	}
	if ci.Ref.CommitSHA != "head-sha-222" {
		t.Errorf("expected head-sha-222, got %s", ci.Ref.CommitSHA)
	}
	if ci.Ref.Branch != "feat/auth" {
		t.Errorf("expected feat/auth, got %s", ci.Ref.Branch)
	}

	// Repo metadata from event payload
	if ci.Repo.ID != "12345" {
		t.Errorf("expected 12345, got %s", ci.Repo.ID)
	}
	if ci.Repo.Name != "myorg/myrepo" {
		t.Errorf("expected myorg/myrepo, got %s", ci.Repo.Name)
	}
	if ci.Repo.DefaultBranch != "develop" {
		t.Errorf("expected develop, got %s", ci.Repo.DefaultBranch)
	}
	if !ci.Repo.Private {
		t.Error("expected Repo.Private true")
	}
}

// --- GitHub Actions: Default branch detection ---

func TestDetectGitContext_GitHubDefaultBranch(t *testing.T) {
	payload := map[string]interface{}{
		"repository": map[string]interface{}{
			"id":             float64(99),
			"full_name":      "org/repo",
			"html_url":       "https://github.com/org/repo",
			"clone_url":      "https://github.com/org/repo.git",
			"default_branch": "main",
		},
	}
	eventFile := filepath.Join(t.TempDir(), "event.json")
	data, _ := json.Marshal(payload)
	os.WriteFile(eventFile, data, 0644)

	envs := map[string]string{
		"GITHUB_ACTIONS":    "true",
		"GITHUB_WORKSPACE":  "/runner/work",
		"GITHUB_SERVER_URL": "https://github.com",
		"GITHUB_REPOSITORY": "org/repo",
		"GITHUB_RUN_ID":     "1",
		"GITHUB_JOB":        "scan",
		"GITHUB_SHA":        "sha",
		"GITHUB_REF":        "refs/heads/main",
		"GITHUB_REF_NAME":   "main",
		"GITHUB_EVENT_PATH": eventFile,
	}
	for k, v := range envs {
		t.Setenv(k, v)
	}

	ci := DetectGitContext()
	if ci == nil {
		t.Fatal("expected CIContext, got nil")
	}
	if !ci.Ref.IsDefault {
		t.Error("expected IsDefault true when branch == default branch")
	}
}

// --- GitHub Actions: Fallback when event payload missing ---

func TestDetectGitContext_GitHubFallback_NoEventPayload(t *testing.T) {
	envs := map[string]string{
		"GITHUB_ACTIONS":       "true",
		"GITHUB_WORKSPACE":     "/runner/work",
		"GITHUB_SERVER_URL":    "https://github.com",
		"GITHUB_REPOSITORY":    "fallback-org/fallback-repo",
		"GITHUB_REPOSITORY_ID": "77777",
		"GITHUB_RUN_ID":        "1",
		"GITHUB_JOB":           "scan",
		"GITHUB_SHA":           "sha-fallback",
		"GITHUB_REF":           "refs/heads/dev",
		"GITHUB_REF_NAME":      "dev",
	}
	for k, v := range envs {
		t.Setenv(k, v)
	}

	ci := DetectGitContext()
	if ci == nil {
		t.Fatal("expected CIContext, got nil")
	}

	// Fallback: repo name from GITHUB_REPOSITORY
	if ci.Repo.Name != "fallback-org/fallback-repo" {
		t.Errorf("expected fallback-org/fallback-repo, got %s", ci.Repo.Name)
	}
	// Fallback: HTML URL constructed from server + repo
	if ci.Repo.HtmlURL != "https://github.com/fallback-org/fallback-repo" {
		t.Errorf("expected constructed HTML URL, got %s", ci.Repo.HtmlURL)
	}
	// Fallback: ID from GITHUB_REPOSITORY_ID
	if ci.Repo.ID != "77777" {
		t.Errorf("expected 77777, got %s", ci.Repo.ID)
	}
}

// --- Parameter Resolution: Full priority chain ---

func TestResolveParamsFromEnv_PriorityChain(t *testing.T) {
	t.Setenv("TEST_PARAM_ENV", "from-env")

	params := []Param{
		StringParam("param1").Env("TEST_PARAM_ENV").Default("from-default").Build(),
	}
	ciParams := map[string]interface{}{
		"param1": "from-ci",
	}

	// Env var wins over ciParams and default
	resolved := resolveParamsFromEnv(params, ciParams)
	if resolved["param1"] != "from-env" {
		t.Errorf("expected env to win, got %v", resolved["param1"])
	}

	// Unset env — ciParams should win over default
	os.Unsetenv("TEST_PARAM_ENV")
	resolved = resolveParamsFromEnv(params, ciParams)
	if resolved["param1"] != "from-ci" {
		t.Errorf("expected ciParams to win, got %v", resolved["param1"])
	}

	// No ciParams — default wins
	resolved = resolveParamsFromEnv(params, nil)
	if resolved["param1"] != "from-default" {
		t.Errorf("expected default, got %v", resolved["param1"])
	}
}

// --- GitLab CI: CommitMessage field ---

func TestDetectGitContext_GitLabCommitMessage(t *testing.T) {
	envs := map[string]string{
		"GITLAB_CI":               "true",
		"CI_PROJECT_DIR":          "/builds/org/repo",
		"CI_JOB_URL":              "https://gitlab.com/org/repo/-/jobs/1",
		"CI_JOB_NAME":             "scan",
		"CI_PROJECT_ID":           "1",
		"CI_PROJECT_PATH":         "org/repo",
		"CI_REPOSITORY_URL":       "https://gitlab.com/org/repo.git",
		"CI_PROJECT_URL":          "https://gitlab.com/org/repo",
		"CI_DEFAULT_BRANCH":       "main",
		"CI_COMMIT_SHA":           "abc",
		"CI_COMMIT_BRANCH":        "feature",
		"CI_COMMIT_MESSAGE":       "Fix: resolve SQL injection vulnerability",
		"CI_COMMIT_REF_PROTECTED": "false",
	}
	for k, v := range envs {
		t.Setenv(k, v)
	}

	ci := DetectGitContext()
	if ci == nil {
		t.Fatal("expected CIContext, got nil")
	}
	if ci.Ref.CommitMessage != "Fix: resolve SQL injection vulnerability" {
		t.Errorf("expected commit message, got %s", ci.Ref.CommitMessage)
	}
}

// --- GitHub Actions: JobURL construction ---

func TestDetectGitContext_GitHubJobURL(t *testing.T) {
	envs := map[string]string{
		"GITHUB_ACTIONS":    "true",
		"GITHUB_WORKSPACE":  "/runner",
		"GITHUB_SERVER_URL": "https://github.com",
		"GITHUB_REPOSITORY": "org/repo",
		"GITHUB_RUN_ID":     "12345",
		"GITHUB_JOB":        "security-scan",
		"GITHUB_SHA":        "sha",
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
	expected := "https://github.com/org/repo/actions/runs/12345"
	if ci.JobURL != expected {
		t.Errorf("expected %s, got %s", expected, ci.JobURL)
	}
	if ci.JobName != "security-scan" {
		t.Errorf("expected security-scan, got %s", ci.JobName)
	}
}

// --- GitLab CI: MR without labels ---

func TestDetectGitContext_GitLabMR_NoLabels(t *testing.T) {
	envs := map[string]string{
		"GITLAB_CI":                           "true",
		"CI_PROJECT_DIR":                      "/builds/org/repo",
		"CI_JOB_URL":                          "https://gitlab.com/org/repo/-/jobs/1",
		"CI_JOB_NAME":                         "scan",
		"CI_PROJECT_ID":                       "1",
		"CI_PROJECT_PATH":                     "org/repo",
		"CI_REPOSITORY_URL":                   "https://gitlab.com/org/repo.git",
		"CI_PROJECT_URL":                      "https://gitlab.com/org/repo",
		"CI_DEFAULT_BRANCH":                   "main",
		"CI_COMMIT_SHA":                       "abc",
		"CI_MERGE_REQUEST_IID":                "10",
		"CI_MERGE_REQUEST_TITLE":              "Update deps",
		"CI_MERGE_REQUEST_TARGET_BRANCH_NAME": "main",
		"CI_MERGE_REQUEST_SOURCE_BRANCH_NAME": "update-deps",
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
	if ci.Ref.PRLabels != nil {
		t.Errorf("expected nil labels, got %v", ci.Ref.PRLabels)
	}
	if ci.Ref.Name != "mr/10" {
		t.Errorf("expected mr/10, got %s", ci.Ref.Name)
	}
}

// --- CI detection priority: GitLab takes precedence over GitHub ---

func TestDetectGitContext_GitLabPrecedence(t *testing.T) {
	// Both CI env vars set — GitLab should win (checked first)
	envs := map[string]string{
		"GITLAB_CI":               "true",
		"GITHUB_ACTIONS":          "true",
		"CI_PROJECT_DIR":          "/builds/org/repo",
		"CI_JOB_URL":              "https://gitlab.com/org/repo/-/jobs/1",
		"CI_JOB_NAME":             "scan",
		"CI_PROJECT_ID":           "1",
		"CI_PROJECT_PATH":         "org/repo",
		"CI_REPOSITORY_URL":       "https://gitlab.com/org/repo.git",
		"CI_PROJECT_URL":          "https://gitlab.com/org/repo",
		"CI_DEFAULT_BRANCH":       "main",
		"CI_COMMIT_SHA":           "abc",
		"CI_COMMIT_BRANCH":        "main",
		"CI_COMMIT_REF_PROTECTED": "false",
	}
	for k, v := range envs {
		t.Setenv(k, v)
	}

	ci := DetectGitContext()
	if ci == nil {
		t.Fatal("expected CIContext, got nil")
	}
	if ci.Source != "gitlab-ci" {
		t.Errorf("expected gitlab-ci precedence, got %s", ci.Source)
	}
}

// --- GitHub PR: Event payload with invalid JSON ---

func TestDetectGitContext_GitHubPR_InvalidEventPayload(t *testing.T) {
	eventFile := filepath.Join(t.TempDir(), "bad-event.json")
	os.WriteFile(eventFile, []byte("not valid json"), 0644)

	envs := map[string]string{
		"GITHUB_ACTIONS":    "true",
		"GITHUB_WORKSPACE":  "/runner/work",
		"GITHUB_SERVER_URL": "https://github.com",
		"GITHUB_REPOSITORY": "org/repo",
		"GITHUB_RUN_ID":     "1",
		"GITHUB_JOB":        "scan",
		"GITHUB_SHA":        "sha",
		"GITHUB_REF":        "refs/pull/7/merge",
		"GITHUB_REF_NAME":   "7/merge",
		"GITHUB_BASE_REF":   "main",
		"GITHUB_HEAD_REF":   "feature",
		"GITHUB_EVENT_PATH": eventFile,
	}
	for k, v := range envs {
		t.Setenv(k, v)
	}

	ci := DetectGitContext()
	if ci == nil {
		t.Fatal("expected CIContext even with bad payload")
	}

	// Should fallback to env vars for PR info
	if ci.Ref.Type != CIRefTypePRMR {
		t.Errorf("expected pr_mr, got %s", ci.Ref.Type)
	}
	if ci.Ref.PRNumber != "7" {
		t.Errorf("expected 7 from regex fallback, got %s", ci.Ref.PRNumber)
	}
	if ci.Ref.BaseBranch != "main" {
		t.Errorf("expected main from GITHUB_BASE_REF fallback, got %s", ci.Ref.BaseBranch)
	}
	if ci.Ref.Branch != "feature" {
		t.Errorf("expected feature from GITHUB_HEAD_REF fallback, got %s", ci.Ref.Branch)
	}
}
