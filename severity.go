package rediver

import (
	scannerv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/scanner/v1"
)

// Severity is the finding severity level (public SDK type — unchanged from v1).
type Severity string

const (
	SeverityCritical Severity = "Critical"
	SeverityHigh     Severity = "High"
	SeverityMedium   Severity = "Medium"
	SeverityLow      Severity = "Low"
	SeverityInfo     Severity = "Info"
	SeverityNone     Severity = "None"
)

// toProtoSeverity converts SDK Severity to the proto enum.
func toProtoSeverity(s Severity) scannerv1.Severity {
	switch s {
	case SeverityCritical:
		return scannerv1.Severity_SEVERITY_CRITICAL
	case SeverityHigh:
		return scannerv1.Severity_SEVERITY_HIGH
	case SeverityMedium:
		return scannerv1.Severity_SEVERITY_MEDIUM
	case SeverityLow:
		return scannerv1.Severity_SEVERITY_LOW
	case SeverityInfo:
		return scannerv1.Severity_SEVERITY_INFO
	default:
		return scannerv1.Severity_SEVERITY_UNSPECIFIED
	}
}

// Confidence represents the confidence level of a finding.
// No proto counterpart exists; internal-only.
type Confidence string

const (
	ConfidenceHigh   Confidence = "High"
	ConfidenceMedium Confidence = "Medium"
	ConfidenceLow    Confidence = "Low"
)

// FindingType is the type of finding (public SDK type).
type FindingType string

const (
	FindingTypeWeb      FindingType = "Web"
	FindingTypeCredLeak FindingType = "CredLeak"
	FindingTypeGit      FindingType = "Git"
	FindingTypeGeneral  FindingType = "General"
)
