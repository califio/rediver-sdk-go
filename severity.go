package rediver

import (
	"github.com/califio/rediver-sdk-go/internal/api"
)

// Severity is a type alias for the generated API Severity enum.
type Severity = api.Severity

const (
	SeverityCritical = api.Critical
	SeverityHigh     = api.High
	SeverityMedium   = api.Medium
	SeverityLow      = api.Low
	SeverityInfo     = api.Info
	SeverityNone     = api.None
)

// Confidence represents the confidence level of a finding.
// No generated API counterpart exists.
type Confidence string

const (
	ConfidenceHigh   Confidence = "High"
	ConfidenceMedium Confidence = "Medium"
	ConfidenceLow    Confidence = "Low"
)

// FindingType is a type alias for the generated API FindingType enum.
type FindingType = api.FindingType

const (
	FindingTypeWeb      = api.Web
	FindingTypeCredLeak = api.CredLeak
	FindingTypeGit      = api.Git
	FindingTypeGeneral  = api.General
)
