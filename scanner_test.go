package rediver

import (
	"context"
	"errors"
	"testing"

	scannerv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/scanner/v1"
)

// --- NewScanner ---

func TestNewScanner_Basic(t *testing.T) {
	s := NewScanner("subdomain", []TargetType{TargetTypeDomain}, func(ctx context.Context, job Job, emit func(Result)) error {
		return nil
	})
	if s.Name() != "subdomain" {
		t.Errorf("name: got %q", s.Name())
	}
	if len(s.AssetTypes()) != 1 || s.AssetTypes()[0] != TargetTypeDomain {
		t.Errorf("asset types: got %v", s.AssetTypes())
	}
	if len(s.Params()) != 0 {
		t.Errorf("expected 0 params, got %d", len(s.Params()))
	}
}

func TestNewScanner_EmptyAssetTypes(t *testing.T) {
	s := NewScanner("all-types", nil, func(ctx context.Context, job Job, emit func(Result)) error {
		return nil
	})
	if len(s.AssetTypes()) != 0 {
		t.Error("nil asset types should return empty")
	}
}

func TestNewScanner_MultipleAssetTypes(t *testing.T) {
	s := NewScanner("multi", []TargetType{TargetTypeDomain, TargetTypeIP, TargetTypeService}, func(ctx context.Context, job Job, emit func(Result)) error {
		return nil
	})
	if len(s.AssetTypes()) != 3 {
		t.Errorf("expected 3 asset types, got %d", len(s.AssetTypes()))
	}
}

// --- WithParam ---

func TestNewScanner_WithParam(t *testing.T) {
	p := StringParam("target").Label("Target").Build()
	s := NewScanner("test", nil, func(ctx context.Context, job Job, emit func(Result)) error {
		return nil
	}, WithParam(p))

	if len(s.Params()) != 1 {
		t.Fatalf("expected 1 param, got %d", len(s.Params()))
	}
	if s.Params()[0].name != "target" {
		t.Errorf("param name: got %q", s.Params()[0].name)
	}
}

func TestNewScanner_WithMultipleParams(t *testing.T) {
	s := NewScanner("test", nil, func(ctx context.Context, job Job, emit func(Result)) error {
		return nil
	},
		WithParam(StringParam("a").Build()),
		WithParam(IntParam("b").Build()),
	)
	if len(s.Params()) != 2 {
		t.Errorf("expected 2 params, got %d", len(s.Params()))
	}
}

// --- WithParams ---

func TestNewScanner_WithParams(t *testing.T) {
	s := NewScanner("test", nil, func(ctx context.Context, job Job, emit func(Result)) error {
		return nil
	}, WithParams(
		StringParam("x").Build(),
		BoolParam("y").Build(),
		IntParam("z").Build(),
	))
	if len(s.Params()) != 3 {
		t.Errorf("expected 3 params, got %d", len(s.Params()))
	}
}

func TestNewScanner_WithParams_OverridesWithParam(t *testing.T) {
	// WithParams should replace, not append
	s := NewScanner("test", nil, func(ctx context.Context, job Job, emit func(Result)) error {
		return nil
	},
		WithParam(StringParam("old").Build()),
		WithParams(StringParam("new").Build()),
	)
	if len(s.Params()) != 1 {
		t.Fatalf("WithParams should replace, got %d params", len(s.Params()))
	}
	if s.Params()[0].name != "new" {
		t.Errorf("expected 'new', got %q", s.Params()[0].name)
	}
}

func TestNewScanner_WithRawParamsSchema(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"audit_mode": map[string]interface{}{"type": "string"},
		},
	}

	s := NewScanner("test", nil, func(ctx context.Context, job Job, emit func(Result)) error {
		return nil
	}, WithRawParamsSchema(schema))

	internal := s.(*scanner)
	if internal.ParamsSchema() == nil {
		t.Fatal("expected params schema to be set")
	}
	props := internal.ParamsSchema()["properties"].(map[string]interface{})
	if _, ok := props["audit_mode"]; !ok {
		t.Error("expected audit_mode property in params schema")
	}
}

// --- WithDisplayName ---

func TestNewScanner_WithDisplayName(t *testing.T) {
	s := NewScanner("vuln_scan", nil, func(ctx context.Context, job Job, emit func(Result)) error {
		return nil
	}, WithDisplayName("Vulnerability Scanner"))

	internal := s.(*scanner)
	if internal.DisplayName() != "Vulnerability Scanner" {
		t.Errorf("display name: got %q", internal.DisplayName())
	}
}

func TestNewScanner_DefaultDisplayName(t *testing.T) {
	s := NewScanner("vuln_scan", nil, func(ctx context.Context, job Job, emit func(Result)) error {
		return nil
	})
	internal := s.(*scanner)
	if internal.DisplayName() != "" {
		t.Errorf("default display name should be empty, got %q", internal.DisplayName())
	}
}

// --- WithRetestHandler ---

func TestNewScanner_WithRetestHandler(t *testing.T) {
	s := NewScanner("test", nil,
		func(ctx context.Context, job Job, emit func(Result)) error {
			return nil
		},
		WithRetestHandler(func(ctx context.Context, job Job, emit func(Result)) error {
			return nil
		}),
	)
	internal := s.(*scanner)
	if !internal.SupportsRetest() {
		t.Error("expected SupportsRetest true")
	}
}

func TestNewScanner_NoRetestHandler(t *testing.T) {
	s := NewScanner("test", nil, func(ctx context.Context, job Job, emit func(Result)) error {
		return nil
	})
	internal := s.(*scanner)
	if internal.SupportsRetest() {
		t.Error("expected SupportsRetest false")
	}
}

// --- Scan behavior ---

func TestScanner_Scan_Discovery_CallsHandler(t *testing.T) {
	var called bool
	s := NewScanner("test", nil, func(ctx context.Context, job Job, emit func(Result)) error {
		called = true
		emit(Domains(Domain{Domain: "found.com"}))
		return nil
	})

	j := newJob(nil) // nil detail → Type() returns Discovery
	var results []Result
	err := s.Scan(context.Background(), j, func(r Result) {
		results = append(results, r)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("handler should have been called")
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestScanner_Scan_HandlerError(t *testing.T) {
	expectedErr := errors.New("scan failed")
	s := NewScanner("test", nil, func(ctx context.Context, job Job, emit func(Result)) error {
		return expectedErr
	})

	err := s.Scan(context.Background(), newJob(nil), func(r Result) {})
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected scan error, got %v", err)
	}
}

func TestScanner_Scan_Retest_WithHandler(t *testing.T) {
	var handlerCalled, retestCalled bool

	s := NewScanner("test", nil,
		func(ctx context.Context, job Job, emit func(Result)) error {
			handlerCalled = true
			return nil
		},
		WithRetestHandler(func(ctx context.Context, job Job, emit func(Result)) error {
			retestCalled = true
			return nil
		}),
	)

	// Create a retest job using proto detail
	j := newJob(&scannerv1.GetJobDetailResponse{Retest: true})

	err := s.Scan(context.Background(), j, func(r Result) {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handlerCalled {
		t.Error("main handler should NOT be called for retest")
	}
	if !retestCalled {
		t.Error("retest handler should be called")
	}
}

func TestScanner_Scan_Retest_NoHandler_SilentReturn(t *testing.T) {
	var handlerCalled bool
	s := NewScanner("test", nil, func(ctx context.Context, job Job, emit func(Result)) error {
		handlerCalled = true
		return nil
	})

	j := newJob(&scannerv1.GetJobDetailResponse{Retest: true})

	err := s.Scan(context.Background(), j, func(r Result) {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handlerCalled {
		t.Error("main handler should NOT be called for retest without retest handler")
	}
}

func TestScanner_Scan_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := NewScanner("test", nil, func(ctx context.Context, job Job, emit func(Result)) error {
		return ctx.Err()
	})

	err := s.Scan(ctx, newJob(nil), func(r Result) {})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestScanner_Scan_MultipleEmits(t *testing.T) {
	s := NewScanner("test", nil, func(ctx context.Context, job Job, emit func(Result)) error {
		emit(Domains(Domain{Domain: "a.com"}))
		emit(Domains(Domain{Domain: "b.com"}))
		emit(Services(Service{Host: "c.com", Port: 80}))
		return nil
	})

	var count int
	err := s.Scan(context.Background(), newJob(nil), func(r Result) {
		count++
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("expected 3 emits, got %d", count)
	}
}

// --- Scanner constants ---

func TestScannerConstants(t *testing.T) {
	if ScannerSubdomain != "subdomain" {
		t.Error("ScannerSubdomain mismatch")
	}
	if ScannerServiceProbe != "service_probe" {
		t.Error("ScannerServiceProbe mismatch")
	}
	if ScannerVulnScan != "vuln_scan" {
		t.Error("ScannerVulnScan mismatch")
	}
	if ScannerSAST != "sast" {
		t.Error("ScannerSAST mismatch")
	}
}
