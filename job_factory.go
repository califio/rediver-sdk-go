package rediver

import (
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

// resolveParams copies the params from the job detail's Struct into a plain map.
func (j *job) resolveParams() {
	if j.detail == nil || j.detail.Params == nil {
		return
	}
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
	type structVal interface {
		GetStringValue() string
		GetNumberValue() float64
		GetBoolValue() bool
		GetKind() interface{}
	}
	return v
}
