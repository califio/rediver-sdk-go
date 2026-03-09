package rediver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// DetectGitContext auto-detects the git context from CI environment variables,
// falling back to local git repository detection.
// Returns nil if not running in CI and not inside a git repository.
func DetectGitContext() *CIContext {
	var ci *CIContext
	if os.Getenv("GITLAB_CI") == "true" {
		ci = detectGitLabCI()
		ci.Source = "gitlab-ci"
	} else if os.Getenv("GITHUB_ACTIONS") == "true" {
		ci = detectGitHubActions()
		ci.Source = "github-action"
	} else {
		// Fallback: detect local git repository
		return detectLocalGit("")
	}

	// Fallback: resolve BaseCommitSHA via git merge-base when CI env var is missing
	if ci.Ref.Type == CIRefTypePRMR && ci.Ref.BaseCommitSHA == "" && ci.Ref.BaseBranch != "" {
		if sha, err := gitExec(context.Background(), ci.RepoDir,
			"merge-base", "origin/"+ci.Ref.BaseBranch, ci.Ref.CommitSHA); err == nil && sha != "" {
			ci.Ref.BaseCommitSHA = sha
		}
	}

	return ci
}

func detectGitLabCI() *CIContext {
	ci := &CIContext{
		RepoDir:  os.Getenv("CI_PROJECT_DIR"),
		JobURL:   os.Getenv("CI_JOB_URL"),
		JobName:  os.Getenv("CI_JOB_NAME"),
		Provider: GitProviderGitLab,
		Repo: CIRepo{
			ID:            os.Getenv("CI_PROJECT_ID"),
			Name:          os.Getenv("CI_PROJECT_PATH"),
			URL:           os.Getenv("CI_REPOSITORY_URL"),
			HtmlURL:       os.Getenv("CI_PROJECT_URL"),
			DefaultBranch: os.Getenv("CI_DEFAULT_BRANCH"),
		},
		Ref: CIRef{
			CommitSHA:     os.Getenv("CI_COMMIT_SHA"),
			CommitMessage: os.Getenv("CI_COMMIT_MESSAGE"),
			IsProtected:   os.Getenv("CI_COMMIT_REF_PROTECTED") == "true",
		},
	}

	// Determine ref type
	mrIID := os.Getenv("CI_MERGE_REQUEST_IID")
	if mrIID != "" {
		ci.Ref.Type = CIRefTypePRMR
		ci.Ref.Name = "mr/" + mrIID
		ci.Ref.PRNumber = mrIID
		ci.Ref.PRTitle = os.Getenv("CI_MERGE_REQUEST_TITLE")
		ci.Ref.BaseBranch = os.Getenv("CI_MERGE_REQUEST_TARGET_BRANCH_NAME")
		ci.Ref.BaseCommitSHA = os.Getenv("CI_MERGE_REQUEST_DIFF_BASE_SHA")
		ci.Ref.Branch = os.Getenv("CI_MERGE_REQUEST_SOURCE_BRANCH_NAME")
		ci.Ref.PRAuthor = os.Getenv("CI_MERGE_REQUEST_AUTHOR")
		if labels := os.Getenv("CI_MERGE_REQUEST_LABELS"); labels != "" {
			ci.Ref.PRLabels = strings.Split(labels, ",")
		}
	} else if branch := os.Getenv("CI_COMMIT_BRANCH"); branch != "" {
		ci.Ref.Type = CIRefTypeBranch
		ci.Ref.Name = branch
		ci.Ref.Branch = branch
		ci.Ref.IsDefault = branch == ci.Repo.DefaultBranch
	} else if tag := os.Getenv("CI_COMMIT_TAG"); tag != "" {
		ci.Ref.Type = CIRefTypeTag
		ci.Ref.Name = tag
	}

	return ci
}

var ghPRRegex = regexp.MustCompile(`^refs/pull/(\d+)/merge$`)

func detectGitHubActions() *CIContext {
	serverURL := os.Getenv("GITHUB_SERVER_URL")
	repo := os.Getenv("GITHUB_REPOSITORY")
	runID := os.Getenv("GITHUB_RUN_ID")

	ci := &CIContext{
		RepoDir:  os.Getenv("GITHUB_WORKSPACE"),
		JobURL:   fmt.Sprintf("%s/%s/actions/runs/%s", serverURL, repo, runID),
		JobName:  os.Getenv("GITHUB_JOB"),
		Provider: GitProviderGitHub,
		Ref: CIRef{
			CommitSHA: os.Getenv("GITHUB_SHA"),
		},
	}

	// GITHUB_EVENT_PATH is the single source of truth — parse everything from it.
	ev := readGitHubEventPayload()

	// Repository metadata
	if repoObj := ghObj(ev, "repository"); repoObj != nil {
		if id, ok := ghFloat64(repoObj, "id"); ok {
			ci.Repo.ID = fmt.Sprintf("%.0f", id)
		}
		ci.Repo.Name = ghStr(repoObj, "full_name")
		ci.Repo.HtmlURL = ghStr(repoObj, "html_url")
		ci.Repo.URL = ghStr(repoObj, "clone_url")
		ci.Repo.DefaultBranch = ghStr(repoObj, "default_branch")
		if private, ok := repoObj["private"].(bool); ok {
			ci.Repo.Private = private
		}
	}

	// Fallbacks when event payload is missing (e.g., workflow_dispatch has minimal payload)
	if ci.Repo.Name == "" {
		ci.Repo.Name = repo
	}
	if ci.Repo.HtmlURL == "" {
		ci.Repo.HtmlURL = fmt.Sprintf("%s/%s", serverURL, repo)
	}
	if ci.Repo.ID == "" {
		if id := os.Getenv("GITHUB_REPOSITORY_ID"); id != "" {
			ci.Repo.ID = id
		} else {
			ci.Repo.ID = repo
		}
	}

	// Determine ref type from GITHUB_REF
	ghRef := os.Getenv("GITHUB_REF")

	if matches := ghPRRegex.FindStringSubmatch(ghRef); len(matches) == 2 {
		ci.Ref.Type = CIRefTypePRMR
		ci.Ref.PRAction = ghStr(ev, "action")

		// All PR metadata from event payload
		if pr := ghObj(ev, "pull_request"); pr != nil {
			if num, ok := ghFloat64(pr, "number"); ok {
				ci.Ref.PRNumber = fmt.Sprintf("%.0f", num)
			}
			ci.Ref.Name = "pr/" + ci.Ref.PRNumber
			ci.Ref.PRTitle = ghStr(pr, "title")
			if draft, ok := pr["draft"].(bool); ok {
				ci.Ref.PRDraft = draft
			}
			if user := ghObj(pr, "user"); user != nil {
				ci.Ref.PRAuthor = ghStr(user, "login")
			}
			if labels, ok := pr["labels"].([]interface{}); ok {
				for _, l := range labels {
					if lm, ok := l.(map[string]interface{}); ok {
						if name := ghStr(lm, "name"); name != "" {
							ci.Ref.PRLabels = append(ci.Ref.PRLabels, name)
						}
					}
				}
			}
			if base := ghObj(pr, "base"); base != nil {
				ci.Ref.BaseCommitSHA = ghStr(base, "sha")
				ci.Ref.BaseBranch = ghStr(base, "ref")
			}
			if head := ghObj(pr, "head"); head != nil {
				ci.Ref.CommitSHA = ghStr(head, "sha")
				ci.Ref.Branch = ghStr(head, "ref")
			}
		}

		// Fallback to env vars if event payload didn't have PR data
		if ci.Ref.PRNumber == "" {
			ci.Ref.PRNumber = matches[1]
			ci.Ref.Name = "pr/" + matches[1]
		}
		if ci.Ref.BaseBranch == "" {
			ci.Ref.BaseBranch = os.Getenv("GITHUB_BASE_REF")
		}
		if ci.Ref.Branch == "" {
			ci.Ref.Branch = os.Getenv("GITHUB_HEAD_REF")
		}
	} else if strings.HasPrefix(ghRef, "refs/tags/") {
		ci.Ref.Type = CIRefTypeTag
		ci.Ref.Name = strings.TrimPrefix(ghRef, "refs/tags/")
	} else {
		ci.Ref.Type = CIRefTypeBranch
		refName := os.Getenv("GITHUB_REF_NAME")
		ci.Ref.Name = refName
		ci.Ref.Branch = refName
		ci.Ref.IsDefault = refName == ci.Repo.DefaultBranch
	}

	return ci
}

// ghObj extracts a nested JSON object by key.
func ghObj(obj map[string]interface{}, key string) map[string]interface{} {
	if obj == nil {
		return nil
	}
	v, _ := obj[key].(map[string]interface{})
	return v
}

// ghStr extracts a string by key, returns "" if missing.
func ghStr(obj map[string]interface{}, key string) string {
	v, _ := obj[key].(string)
	return v
}

// ghFloat64 extracts a float64 by key (json.Unmarshal decodes numbers as float64).
func ghFloat64(obj map[string]interface{}, key string) (float64, bool) {
	v, ok := obj[key].(float64)
	return v, ok
}

// readGitHubEventPayload reads the JSON event payload from GITHUB_EVENT_PATH.
func readGitHubEventPayload() map[string]interface{} {
	path := os.Getenv("GITHUB_EVENT_PATH")
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}
	return payload
}
