package rediver

import (
	"context"
	"fmt"

	commonv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/common/v1"
	"github.com/califio/rediver-sdk-go/utils"
)

// Repository returns git repository info (for CI/SAST jobs), populated with
// any resolved SHAs from a prior prepareRepository call.
func (j *job) Repository() (*Repository, bool) {
	if j.detail == nil || j.detail.Target == nil || j.detail.Target.Repository == nil {
		return nil, false
	}
	r := j.detail.Target.Repository
	target := r.GetTarget()
	if target == nil {
		return nil, false
	}
	repo := &Repository{
		URL:           target.GetUrl(),
		Event:         ciEventToString(target.GetEvent()),
		Branch:        target.GetBranch(),
		CommitSHA:     target.GetCommitSha(),
		BaseBranch:    target.GetBaseBranch(),
		BaseCommitSHA: target.GetBaseCommitSha(),
		PrNumber:      int(target.GetPullRequestNumber()),
		ArtifactID:    r.GetArtifactId(),
		DiffOnly:      r.GetDiffOnly(),
	}
	repo.Provider = gitProviderToString(target.GetProvider())
	if cred := r.GetCredential(); cred != nil {
		repo.Username = cred.GetUsername()
		repo.Password = cred.GetPassword()
	}
	if tag := target.GetTag(); tag != "" {
		repo.Ref = "refs/tags/" + tag
	} else if branch := target.GetBranch(); branch != "" {
		repo.Ref = "refs/heads/" + branch
	}

	// Populate CommitSHA from resolved HEAD when server didn't provide it.
	if repo.CommitSHA == "" && j.resolvedHeadSHA != "" {
		repo.CommitSHA = j.resolvedHeadSHA
	}
	// Populate BaseCommitSHA from resolved merge-base when server didn't provide it.
	if repo.BaseCommitSHA == "" && j.resolvedBaseSHA != "" {
		repo.BaseCommitSHA = j.resolvedBaseSHA
	}
	return repo, true
}

// ciEventToString converts a proto CiEvent enum to the string used by scanner logic.
func ciEventToString(e commonv1.CiEvent) string {
	switch e {
	case commonv1.CiEvent_CI_EVENT_PUSH:
		return "push"
	case commonv1.CiEvent_CI_EVENT_PULL_REQUEST:
		return "pull_request"
	case commonv1.CiEvent_CI_EVENT_TAG:
		return "tag"
	default:
		return ""
	}
}

// gitProviderToString converts a proto GitProvider enum to a lowercase provider string.
func gitProviderToString(p commonv1.GitProvider) string {
	switch p {
	case commonv1.GitProvider_GIT_PROVIDER_GITLAB:
		return "gitlab"
	case commonv1.GitProvider_GIT_PROVIDER_GITHUB:
		return "github"
	case commonv1.GitProvider_GIT_PROVIDER_BITBUCKET:
		return "bitbucket"
	default:
		return ""
	}
}

// RepoDir returns the path to the prepared repository working directory.
func (j *job) RepoDir() string {
	return j.repoDir
}

// ChangedFiles returns files changed between base and head commits.
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

// Integration returns third-party integration tokens for the job.
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
