package utils

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ChangedFiles represents files changed between two commits.
type ChangedFiles struct {
	Added    []string
	Modified []string
	Deleted  []string
}

// CheckoutOptions configures git checkout behavior.
type CheckoutOptions struct {
	WorkDir     string
	RepoURL     string
	Refs        []string
	CheckoutRef string
}

// GitCheckout clones a repository and checks out a specific ref.
func GitCheckout(ctx context.Context, opts CheckoutOptions) error {
	// Ensure workdir exists
	if err := os.MkdirAll(opts.WorkDir, 0755); err != nil {
		return err
	}

	// Initialize repo if not exists
	gitDir := filepath.Join(opts.WorkDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		if _, err := Exec(ctx, "git", []string{"init"}, WithWorkDir(opts.WorkDir)); err != nil {
			return err
		}
		if _, err := Exec(ctx, "git", []string{"remote", "add", "origin", opts.RepoURL}, WithWorkDir(opts.WorkDir)); err != nil {
			return err
		}
	}

	// Fetch refs
	GitFetch(ctx, opts.WorkDir, opts.Refs...)

	// Checkout
	_, err := Exec(ctx, "git", []string{"checkout", "--force", opts.CheckoutRef}, WithWorkDir(opts.WorkDir))
	return err
}

// GitFetch fetches specific refs from origin.
func GitFetch(ctx context.Context, repoPath string, refs ...string) {
	baseArgs := []string{"-c", "protocol.version=2", "fetch", "--depth=1", "--prune", "--no-recurse-submodules", "--no-tags", "origin"}

	for _, ref := range refs {
		args := append(baseArgs, ref)
		_, _ = Exec(ctx, "git", args, WithWorkDir(repoPath))
	}
}

// GitMergeBase finds the common ancestor of two commits.
// Returns the merge-base SHA, or empty string + error if not found.
func GitMergeBase(ctx context.Context, repoPath, ref1, ref2 string) (string, error) {
	var lines []string
	_, err := Exec(ctx, "git", []string{"merge-base", ref1, ref2},
		WithWorkDir(repoPath),
		WithStdout(func(line string) { lines = append(lines, line) }),
	)
	if err != nil {
		return "", err
	}
	result := strings.Join(lines, "")
	return strings.TrimSpace(result), nil
}

// GitDiff returns files changed between two commits.
func GitDiff(ctx context.Context, repoPath, baseCommit, headCommit string) (*ChangedFiles, error) {
	if baseCommit == "" {
		return nil, errors.New("base commit is required")
	}
	if headCommit == "" {
		return nil, errors.New("head commit is required")
	}

	var outputLines []string
	_, err := Exec(ctx, "git",
		[]string{"diff", "--name-status", baseCommit, headCommit},
		WithWorkDir(repoPath),
		WithStdout(func(line string) {
			outputLines = append(outputLines, line)
		}),
	)
	if err != nil {
		return nil, err
	}

	result := &ChangedFiles{}
	for _, line := range outputLines {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		status, file := parts[0], parts[1]
		switch status {
		case "A":
			result.Added = append(result.Added, file)
		case "M":
			result.Modified = append(result.Modified, file)
		case "D":
			result.Deleted = append(result.Deleted, file)
		}
	}

	return result, nil
}
