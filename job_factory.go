package rediver

import (
	commonv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/common/v1"
	scannerv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/scanner/v1"
)

// newJob creates a Job from a GetJobDetailResponse proto (standard dispatch mode).
func newJob(detail *scannerv1.GetJobDetailResponse) Job {
	j := &job{detail: detail}
	if detail != nil {
		j.resolveParams()
	}
	return j
}

// newCIJob creates a Job from CIContext data (CI/local mode). No GetJobDetailResponse needed.
func newCIJob(jobID string, ci *CIContext, scannerName string, params map[string]interface{}) Job {
	// Map CI ref type to proto CiEvent.
	ciEvent := commonv1.CiEvent_CI_EVENT_PUSH
	if ci.Ref.Type == CIRefTypePRMR {
		ciEvent = commonv1.CiEvent_CI_EVENT_PULL_REQUEST
	}

	repoCtx := &commonv1.RepositoryTarget{
		Url:    ci.Repo.URL,
		Event:  ciEvent,
		Branch: strOptionalVal(ci.Ref.Branch),
	}
	if ci.Ref.CommitSHA != "" {
		repoCtx.CommitSha = &ci.Ref.CommitSHA
	}
	if ci.Ref.BaseCommitSHA != "" {
		repoCtx.BaseCommitSha = &ci.Ref.BaseCommitSHA
	}
	if ci.Ref.BaseBranch != "" {
		repoCtx.BaseBranch = &ci.Ref.BaseBranch
	}

	detail := &scannerv1.GetJobDetailResponse{
		Id:      jobID,
		Scanner: scannerName,
		Target: &scannerv1.JobTarget{
			Repository: &scannerv1.RepositoryJobTarget{Target: repoCtx},
		},
	}
	if len(params) > 0 {
		// params stored separately; GetJobDetailResponse uses google.protobuf.Struct.
		// We store params as the plain map and skip setting detail.Params.
	}

	j := &job{
		detail:    detail,
		ciContext: ci,
		params:    params,
	}
	return j
}

// resolveParams copies the params from the job detail's Struct into a plain map.
func (j *job) resolveParams() {
	if j.detail == nil || j.detail.Params == nil {
		return
	}
	// google.protobuf.Struct fields are map[string]*structpb.Value
	j.params = make(map[string]interface{})
	for k, v := range j.detail.Params.GetFields() {
		j.params[k] = structValueToInterface(v)
	}
}

// structValueToInterface converts a protobuf Struct value to a Go interface{}.
func structValueToInterface(v interface {
	GetStringValue() string
	GetNumberValue() float64
	GetBoolValue() bool
}) interface{} {
	// Use duck-typed interface for structpb.Value methods.
	type structVal interface {
		GetStringValue() string
		GetNumberValue() float64
		GetBoolValue() bool
		GetKind() interface{}
	}
	return v
}
