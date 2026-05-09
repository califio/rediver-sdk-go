package rediver

import scannerv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/scanner/v1"

// ciContextToProtoRequest converts a CIContext + scanner info into the
// CreateCiJobRequest proto message.
func ciContextToProtoRequest(ci *CIContext, scannerName string) *scannerv1.CreateCiJobRequest {
	refType := scannerv1.GitRefType_GIT_REF_TYPE_BRANCH
	switch ci.Ref.Type {
	case CIRefTypeTag:
		refType = scannerv1.GitRefType_GIT_REF_TYPE_TAG
	case CIRefTypePRMR:
		refType = scannerv1.GitRefType_GIT_REF_TYPE_PR_MR
	}

	ref := &scannerv1.CiJobGitRef{
		Type:          refType,
		Name:          ci.Ref.Name,
		HeadCommitSha: ci.Ref.CommitSHA,
		Branch:        strOptionalVal(ci.Ref.Branch),
		BaseBranch:    strOptionalVal(ci.Ref.BaseBranch),
		IsProtected:   ci.Ref.IsProtected,
		IsDefault:     ci.Ref.IsDefault,
		PrTitle:       strOptionalVal(ci.Ref.PRTitle),
	}
	if ci.Ref.BaseCommitSHA != "" {
		ref.BaseCommitSha = &ci.Ref.BaseCommitSHA
	}

	repo := &scannerv1.CiJobGitRepo{
		Id:            ci.Repo.ID,
		Name:          ci.Repo.Name,
		CloneUrl:      ci.Repo.URL,
		HtmlUrl:       strOptionalVal(ci.Repo.HtmlURL),
		DefaultBranch: strOptionalVal(ci.Repo.DefaultBranch),
		Private:       ci.Repo.Private,
	}

	req := &scannerv1.CreateCiJobRequest{
		Name:     scannerName,
		Scanner:  scannerName,
		Provider: toProtoGitProvider(ci.Provider),
		Ref:      ref,
		Repo:     repo,
	}
	if ci.JobURL != "" {
		req.JobUrl = &ci.JobURL
	}
	if ci.Source != "" {
		req.Source = &ci.Source
	}

	return req
}
