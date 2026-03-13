package rediver

import (
	"github.com/califio/rediver-sdk-go/internal/api"
)

// ciContextToAPIRequest converts a CIContext + scanner info into the generated API request.
// Uses the CreateCiJobRequest type (maps to POST /api/agent/job/create via CreateJobWithResponse).
func ciContextToAPIRequest(ci *CIContext, scannerName string) api.CreateCiJobRequest {
	provider := ci.Provider
	refType := api.Branch
	switch ci.Ref.Type {
	case CIRefTypeTag:
		refType = api.Tag
	case CIRefTypePRMR:
		refType = api.PRMR
	}

	cloneURL := ci.Repo.URL
	private := ci.Repo.Private

	ref := api.CommitRef{
		Type:          &refType,
		Name:          ci.Ref.Name,
		HeadCommitSha: ci.Ref.CommitSHA,
		BaseCommitSha: ci.Ref.BaseCommitSHA,
		CommitMessage: nilIfEmpty(ci.Ref.CommitMessage),
		Branch:        ci.Ref.Branch,
		BaseBranch:    ci.Ref.BaseBranch,
		IsProtected:   &ci.Ref.IsProtected,
		IsDefault:     &ci.Ref.IsDefault,
		PrTitle:       ci.Ref.PRTitle,
		PrNumber:      ci.Ref.PRNumber,
	}

	repo := api.GitRepo{
		Id:            ci.Repo.ID,
		Name:          ci.Repo.Name,
		CloneUrl:      &cloneURL,
		HtmlUrl:       nilIfEmpty(ci.Repo.HtmlURL),
		DefaultBranch: nilIfEmpty(ci.Repo.DefaultBranch),
		Private:       &private,
		Provider:      &provider,
	}

	return api.CreateCiJobRequest{
		Name:     scannerName,
		Scanner:  scannerName,
		Provider: &provider,
		JobUrl:   nilIfEmpty(ci.JobURL),
		Source:   nilIfEmpty(ci.Source),
		Ref:      &ref,
		Repo:     &repo,
	}
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
