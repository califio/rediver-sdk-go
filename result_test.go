package rediver

import "testing"

// --- Domains ---

func TestDomains_Empty(t *testing.T) {
	r := Domains()
	if len(r.GetDomains()) != 0 {
		t.Errorf("expected 0 domains, got %d", len(r.GetDomains()))
	}
	if r.HasData() {
		t.Error("empty Domains() should not have data")
	}
}

func TestDomains_Single(t *testing.T) {
	r := Domains(Domain{Domain: "example.com", A: []string{"1.2.3.4"}})
	domains := r.GetDomains()
	if len(domains) != 1 {
		t.Fatalf("expected 1 domain, got %d", len(domains))
	}
	if domains[0].Domain != "example.com" {
		t.Errorf("domain: got %q", domains[0].Domain)
	}
	if !r.HasData() {
		t.Error("should have data")
	}
}

func TestDomains_Multiple(t *testing.T) {
	r := Domains(
		Domain{Domain: "a.com"},
		Domain{Domain: "b.com"},
		Domain{Domain: "c.com"},
	)
	if len(r.GetDomains()) != 3 {
		t.Errorf("expected 3 domains, got %d", len(r.GetDomains()))
	}
}

func TestDomains_DoesNotReturnFindings(t *testing.T) {
	r := Domains(Domain{Domain: "x.com"})
	if len(r.GetWebFindings()) != 0 {
		t.Error("Domains result should have no web findings")
	}
	if len(r.GetSASTFindings()) != 0 {
		t.Error("Domains result should have no SAST findings")
	}
	if len(r.GetServices()) != 0 {
		t.Error("Domains result should have no services")
	}
}

// --- Services ---

func TestServices_Empty(t *testing.T) {
	r := Services()
	if r.HasData() {
		t.Error("empty Services() should not have data")
	}
}

func TestServices_Single(t *testing.T) {
	r := Services(Service{Host: "10.0.0.1", Port: 80, ServiceName: "http"})
	services := r.GetServices()
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}
	if services[0].Host != "10.0.0.1" {
		t.Errorf("host: got %q", services[0].Host)
	}
	if services[0].Port != 80 {
		t.Errorf("port: got %d", services[0].Port)
	}
	if !r.HasData() {
		t.Error("should have data")
	}
}

func TestServices_WithHTTPAndCert(t *testing.T) {
	r := Services(Service{
		Host: "example.com",
		Port: 443,
		HTTP: &HTTPInfo{
			URL:        "https://example.com",
			StatusCode: 200,
			Title:      "Example",
		},
		Certificate: &TLSInfo{
			SubjectCN:  "*.example.com",
			IsWildcard: true,
		},
	})
	s := r.GetServices()[0]
	if s.HTTP == nil {
		t.Fatal("expected HTTP info")
	}
	if s.HTTP.StatusCode != 200 {
		t.Errorf("status: got %d", s.HTTP.StatusCode)
	}
	if s.Certificate == nil {
		t.Fatal("expected TLS info")
	}
	if !s.Certificate.IsWildcard {
		t.Error("expected wildcard")
	}
}

// --- WebFindings ---

func TestWebFindings_Empty(t *testing.T) {
	r := WebFindings()
	if r.HasData() {
		t.Error("empty WebFindings() should not have data")
	}
	if len(r.GetWebFindings()) != 0 {
		t.Error("expected 0 web findings")
	}
}

func TestWebFindings_Single(t *testing.T) {
	r := WebFindings(WebFinding{
		Name:     "SQL Injection",
		Severity: SeverityCritical,
		Endpoint: "https://example.com/login",
	})
	findings := r.GetWebFindings()
	if len(findings) != 1 {
		t.Fatalf("expected 1, got %d", len(findings))
	}
	if findings[0].Name != "SQL Injection" {
		t.Errorf("name: got %q", findings[0].Name)
	}
	if findings[0].Severity != SeverityCritical {
		t.Errorf("severity: got %v", findings[0].Severity)
	}
	if !r.HasData() {
		t.Error("should have data")
	}
}

func TestWebFindings_DoesNotReturnSAST(t *testing.T) {
	r := WebFindings(WebFinding{Name: "XSS"})
	if len(r.GetSASTFindings()) != 0 {
		t.Error("WebFindings should not return SAST findings")
	}
}

func TestWebFindings_WithRequests(t *testing.T) {
	r := WebFindings(WebFinding{
		Name: "SSRF",
		Requests: []HTTPRequest{
			{RawRequest: "GET /api HTTP/1.1", RawResponse: "HTTP/1.1 200 OK"},
			{RawRequest: "POST /callback HTTP/1.1", RawResponse: "HTTP/1.1 302 Found"},
		},
	})
	f := r.GetWebFindings()[0]
	if len(f.Requests) != 2 {
		t.Errorf("expected 2 requests, got %d", len(f.Requests))
	}
}

// --- SASTFindings ---

func TestSASTFindings_Empty(t *testing.T) {
	r := SASTFindings()
	if r.HasData() {
		t.Error("empty SASTFindings() should not have data")
	}
}

func TestSASTFindings_Single(t *testing.T) {
	r := SASTFindings(SASTFinding{
		Name:      "Hardcoded Secret",
		Severity:  SeverityHigh,
		File:      "config/db.go",
		StartLine: 42,
		EndLine:   42,
		Snippet:   `password := "admin123"`,
	})
	findings := r.GetSASTFindings()
	if len(findings) != 1 {
		t.Fatalf("expected 1, got %d", len(findings))
	}
	if findings[0].File != "config/db.go" {
		t.Errorf("file: got %q", findings[0].File)
	}
	if findings[0].StartLine != 42 {
		t.Errorf("start: got %d", findings[0].StartLine)
	}
}

func TestSASTFindings_DoesNotReturnWeb(t *testing.T) {
	r := SASTFindings(SASTFinding{Name: "leak"})
	if len(r.GetWebFindings()) != 0 {
		t.Error("SASTFindings should not return web findings")
	}
}

func TestSASTFindings_WithCodeFlows(t *testing.T) {
	r := SASTFindings(SASTFinding{
		Name: "Taint",
		CodeFlows: []CodeFlowNode{
			{File: "a.go", StartLine: 1, Message: "source"},
			{File: "b.go", StartLine: 5, Message: "sink"},
		},
	})
	f := r.GetSASTFindings()[0]
	if len(f.CodeFlows) != 2 {
		t.Errorf("expected 2 code flow nodes, got %d", len(f.CodeFlows))
	}
}

func TestSASTFindings_WithCodeLines(t *testing.T) {
	r := SASTFindings(SASTFinding{
		Name: "Bug",
		CodeLines: []CodeLine{
			{Line: 10, Content: "func main() {"},
			{Line: 11, Content: "    exec(input)"},
			{Line: 12, Content: "}"},
		},
	})
	f := r.GetSASTFindings()[0]
	if len(f.CodeLines) != 3 {
		t.Errorf("expected 3 code lines, got %d", len(f.CodeLines))
	}
	if f.CodeLines[1].Line != 11 {
		t.Errorf("expected line 11, got %d", f.CodeLines[1].Line)
	}
}

// --- Mixed results ---

func TestWebFindings_Multiple_MixedSeverity(t *testing.T) {
	r := WebFindings(
		WebFinding{Name: "Critical", Severity: SeverityCritical},
		WebFinding{Name: "Info", Severity: SeverityInfo},
		WebFinding{Name: "None", Severity: SeverityNone},
	)
	findings := r.GetWebFindings()
	if len(findings) != 3 {
		t.Fatalf("expected 3, got %d", len(findings))
	}
	if findings[0].Severity != SeverityCritical {
		t.Errorf("first should be critical")
	}
	if findings[2].Severity != SeverityNone {
		t.Errorf("last should be none")
	}
}

// --- HasData ---

func TestHasData_EmptyResult(t *testing.T) {
	r := Result{}
	if r.HasData() {
		t.Error("zero-value Result should not have data")
	}
}

func TestHasData_OnlyDomains(t *testing.T) {
	r := Domains(Domain{Domain: "x.com"})
	if !r.HasData() {
		t.Error("should have data with domain")
	}
}

func TestHasData_OnlyFindings(t *testing.T) {
	r := WebFindings(WebFinding{Name: "XSS"})
	if !r.HasData() {
		t.Error("should have data with finding")
	}
}

// --- GetWebFindings / GetSASTFindings with mixed internal findings ---

func TestGetWebFindings_SkipsSASTEntries(t *testing.T) {
	// Manually construct a Result with mixed finding types
	r := Result{
		findings: []finding{
			{web: &WebFinding{Name: "web1"}},
			{sast: &SASTFinding{Name: "sast1"}},
			{web: &WebFinding{Name: "web2"}},
		},
	}
	web := r.GetWebFindings()
	if len(web) != 2 {
		t.Fatalf("expected 2 web findings, got %d", len(web))
	}
	if web[0].Name != "web1" || web[1].Name != "web2" {
		t.Errorf("wrong web findings: %v", web)
	}
}

func TestGetSASTFindings_SkipsWebEntries(t *testing.T) {
	r := Result{
		findings: []finding{
			{sast: &SASTFinding{Name: "sast1"}},
			{web: &WebFinding{Name: "web1"}},
			{sast: &SASTFinding{Name: "sast2"}},
		},
	}
	sast := r.GetSASTFindings()
	if len(sast) != 2 {
		t.Fatalf("expected 2 SAST findings, got %d", len(sast))
	}
	if sast[0].Name != "sast1" || sast[1].Name != "sast2" {
		t.Errorf("wrong SAST findings: %v", sast)
	}
}

// --- Edge case: finding with neither web nor sast set ---

func TestGetFindings_EmptyFindingEntry(t *testing.T) {
	r := Result{
		findings: []finding{
			{}, // neither web nor sast
		},
	}
	if len(r.GetWebFindings()) != 0 {
		t.Error("should skip empty finding for web")
	}
	if len(r.GetSASTFindings()) != 0 {
		t.Error("should skip empty finding for SAST")
	}
	// But HasData should still be true since findings slice is non-empty
	if !r.HasData() {
		t.Error("HasData should be true (findings slice is non-empty)")
	}
}
