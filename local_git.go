package rediver

import (
	"context"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/califio/rediver-sdk-go/utils"
)

// detectLocalGit detects git repository info from the local filesystem.
// workDir is the directory to check; if empty, uses current directory.
// Returns nil if not inside a git repository.
func detectLocalGit(workDir string) *CIContext {
	ctx := context.Background()

	// Verify we're in a git repo
	repoDir, err := gitExec(ctx, workDir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil
	}

	// Commit SHA (required)
	commitSHA, err := gitExec(ctx, repoDir, "rev-parse", "HEAD")
	if err != nil {
		return nil
	}

	// Branch name
	branch, _ := gitExec(ctx, repoDir, "rev-parse", "--abbrev-ref", "HEAD")
	if branch == "HEAD" {
		branch = "" // detached HEAD
	}

	// Remote URL (optional)
	remoteURL, _ := gitExec(ctx, repoDir, "remote", "get-url", "origin")

	// Default branch (optional)
	defaultBranch := detectDefaultBranch(ctx, repoDir)

	// Derive provider from remote URL
	provider := parseProviderFromURL(remoteURL)

	// Derive repo name from remote URL or directory name
	repoName := parseRepoName(remoteURL, repoDir)

	return &CIContext{
		RepoDir:  repoDir,
		Source:   "local",
		Provider: provider,
		Repo: CIRepo{
			ID:            repoName,
			Name:          repoName,
			URL:           remoteURL,
			HtmlURL:       remoteToHtmlURL(remoteURL),
			DefaultBranch: defaultBranch,
		},
		Ref: CIRef{
			Type:      CIRefTypeBranch,
			Name:      branch,
			CommitSHA: commitSHA,
			Branch:    branch,
			IsDefault: branch != "" && branch == defaultBranch,
		},
	}
}

// parseProviderFromURL extracts GitProvider from a remote URL.
// Supports HTTPS and SSH formats.
func parseProviderFromURL(remoteURL string) GitProvider {
	if remoteURL == "" {
		return GitProviderUnknown
	}
	host := extractHostFromGitURL(remoteURL)
	switch {
	case strings.Contains(host, "github"):
		return GitProviderGitHub
	case strings.Contains(host, "gitlab"):
		return GitProviderGitLab
	case strings.Contains(host, "bitbucket"):
		return GitProviderBitbucket
	default:
		return GitProviderUnknown
	}
}

// extractHostFromGitURL handles both HTTPS and SSH git URLs.
func extractHostFromGitURL(rawURL string) string {
	// SSH format: git@github.com:org/repo.git
	if strings.HasPrefix(rawURL, "git@") {
		parts := strings.SplitN(rawURL, ":", 2)
		if len(parts) == 2 {
			return strings.TrimPrefix(parts[0], "git@")
		}
	}
	// HTTPS format
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		return u.Host
	}
	return ""
}

// parseRepoName extracts "org/repo" from remote URL, or uses dir basename.
func parseRepoName(remoteURL, repoDir string) string {
	if remoteURL != "" {
		// SSH format: git@host:org/repo.git
		if strings.HasPrefix(remoteURL, "git@") {
			parts := strings.SplitN(remoteURL, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSuffix(parts[1], ".git")
			}
		}
		// HTTPS format
		if u, err := url.Parse(remoteURL); err == nil {
			path := strings.TrimPrefix(u.Path, "/")
			return strings.TrimSuffix(path, ".git")
		}
	}
	return filepath.Base(repoDir)
}

// remoteToHtmlURL converts a git remote URL to an HTTPS browsable URL.
func remoteToHtmlURL(remoteURL string) string {
	if remoteURL == "" {
		return ""
	}
	// SSH: git@github.com:org/repo.git -> https://github.com/org/repo
	if strings.HasPrefix(remoteURL, "git@") {
		parts := strings.SplitN(remoteURL, ":", 2)
		if len(parts) == 2 {
			host := strings.TrimPrefix(parts[0], "git@")
			path := strings.TrimSuffix(parts[1], ".git")
			return "https://" + host + "/" + path
		}
	}
	// HTTPS: strip .git suffix
	return strings.TrimSuffix(remoteURL, ".git")
}

// detectDefaultBranch tries to determine the default branch from origin HEAD.
func detectDefaultBranch(ctx context.Context, repoDir string) string {
	ref, err := gitExec(ctx, repoDir, "symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil {
		return strings.TrimPrefix(ref, "refs/remotes/origin/")
	}
	return ""
}

// gitExec runs a git command and returns trimmed stdout.
func gitExec(ctx context.Context, workDir string, args ...string) (string, error) {
	var lines []string
	_, err := utils.Exec(ctx, "git", args,
		utils.WithWorkDir(workDir),
		utils.WithStdout(func(line string) { lines = append(lines, line) }),
	)
	if err != nil {
		return "", err
	}
	output := strings.Join(lines, "\n")
	return strings.TrimSpace(output), nil
}
