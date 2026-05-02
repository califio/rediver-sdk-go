package rediver

import (
	"context"

	"github.com/califio/rediver-sdk-go/utils"
)

// resolveCommitSHAs fills j.resolvedHeadSHA and j.resolvedBaseSHA after a successful
// git checkout. It is called by prepareRepository and is a no-op when the server has
// already supplied both SHA values.
func (j *job) resolveCommitSHAs(ctx context.Context, repo *Repository, workDir string) {
	// Resolve HEAD SHA when server didn't supply it.
	if repo.CommitSHA == "" {
		if sha, err := utils.GitRevParseHead(ctx, workDir); err == nil && sha != "" {
			j.resolvedHeadSHA = sha
		}
	}

	// Resolve merge-base SHA for MR/PR events when server didn't supply it.
	if (repo.Event == "merge_request" || repo.Event == "pull_request") && repo.BaseBranch != "" {
		if sha, err := utils.GitMergeBase(ctx, workDir, "origin/"+repo.BaseBranch, "HEAD"); err == nil && sha != "" {
			j.resolvedBaseSHA = sha
		}
	}
}
