package rediver

import "context"

// Scanner defines the interface that all scanners must implement.
type Scanner interface {
	// Name returns the unique identifier for this scanner.
	// This must match the scanner name registered in the Rediver backend.
	Name() string

	// Params returns the configurable parameters for this scanner.
	// These are displayed in the Rediver UI for user configuration.
	Params() []Param

	// AssetTypes returns the target types this scanner can handle.
	// Empty means the scanner accepts all types (no restriction).
	AssetTypes() []TargetType

	// Scan executes a scan job.
	// Results should be sent through the emit callback.
	// The callback can be called multiple times for streaming results.
	// Return an error only for fatal failures that prevent scanning.
	Scan(ctx context.Context, job Job, emit func(Result)) error
}

// ScanFunc is a convenience type for simple scanners.
// Use this when you don't need the full Scanner interface.
type ScanFunc func(ctx context.Context, job Job, emit func(Result)) error

// scanner is the internal implementation of Scanner.
type scanner struct {
	name            string
	displayName     string // human-readable name, empty = use name
	params          []Param
	handler         ScanFunc
	retestHandler   ScanFunc // optional, for retest jobs
	assetTypes      []TargetType
}

// NewScanner creates a Scanner from a name, asset types, and handler function.
func NewScanner(name string, assetTypes []TargetType, handler ScanFunc, opts ...ScannerOption) Scanner {
	s := &scanner{
		name:       name,
		assetTypes: assetTypes,
		handler:    handler,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ScannerOption configures a Scanner.
type ScannerOption func(*scanner)

// WithParam adds a parameter to the scanner.
func WithParam(param Param) ScannerOption {
	return func(s *scanner) {
		s.params = append(s.params, param)
	}
}

// WithParams sets all parameters for the scanner.
func WithParams(params ...Param) ScannerOption {
	return func(s *scanner) {
		s.params = params
	}
}

// WithDisplayName sets a human-readable display name for the scanner.
// If not set, the scanner name is used as the display name.
func WithDisplayName(name string) ScannerOption {
	return func(s *scanner) {
		s.displayName = name
	}
}

// WithRetestHandler sets a separate handler for retest jobs.
// If not set and the job is retest, the SDK completes the job with no results.
func WithRetestHandler(handler ScanFunc) ScannerOption {
	return func(s *scanner) {
		s.retestHandler = handler
	}
}

// SupportsRetest returns true if this scanner has a retest handler.
func (s *scanner) SupportsRetest() bool {
	return s.retestHandler != nil
}

func (s *scanner) Name() string {
	return s.name
}

func (s *scanner) DisplayName() string {
	return s.displayName
}

func (s *scanner) Params() []Param {
	return s.params
}

func (s *scanner) AssetTypes() []TargetType {
	return s.assetTypes
}

func (s *scanner) Scan(ctx context.Context, job Job, emit func(Result)) error {
	if job.Type() == JobTypeRetest {
		if s.retestHandler != nil {
			return s.retestHandler(ctx, job, emit)
		}
		// No retest handler — complete silently
		return nil
	}
	return s.handler(ctx, job, emit)
}

// Common scanner type constants.
const (
	ScannerSubdomain    = "subdomain"
	ScannerServiceProbe = "service_probe"
	ScannerVulnScan     = "vuln_scan"
	ScannerSAST         = "sast"
)
