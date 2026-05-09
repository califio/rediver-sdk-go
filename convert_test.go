package rediver

import (
	"testing"
	"time"

	scannerv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/scanner/v1"
)

// --- ptrOrNil ---

func TestPtrOrNil_Empty(t *testing.T) {
	if ptrOrNil("") != nil {
		t.Error("empty string should return nil")
	}
}

func TestPtrOrNil_NonEmpty(t *testing.T) {
	p := ptrOrNil("hello")
	if p == nil || *p != "hello" {
		t.Errorf("expected pointer to 'hello', got %v", p)
	}
}

// --- ptrSliceOrNil ---

func TestPtrSliceOrNil_Nil(t *testing.T) {
	if ptrSliceOrNil(nil) != nil {
		t.Error("nil slice should return nil")
	}
}

func TestPtrSliceOrNil_Empty(t *testing.T) {
	if ptrSliceOrNil([]string{}) != nil {
		t.Error("empty slice should return nil")
	}
}

func TestPtrSliceOrNil_NonEmpty(t *testing.T) {
	p := ptrSliceOrNil([]string{"a", "b"})
	if p == nil || len(*p) != 2 {
		t.Errorf("expected pointer to [a b], got %v", p)
	}
}

// --- ptrInt32OrNil ---

func TestPtrInt32OrNil_Zero(t *testing.T) {
	if ptrInt32OrNil(0) != nil {
		t.Error("zero should return nil")
	}
}

func TestPtrInt32OrNil_Positive(t *testing.T) {
	p := ptrInt32OrNil(443)
	if p == nil || *p != 443 {
		t.Errorf("expected 443, got %v", p)
	}
}

func TestPtrInt32OrNil_Negative(t *testing.T) {
	p := ptrInt32OrNil(-1)
	if p == nil || *p != -1 {
		t.Errorf("expected -1, got %v", p)
	}
}

// --- ptrFloat32OrNil ---

func TestPtrFloat32OrNil_Zero(t *testing.T) {
	if ptrFloat32OrNil(0) != nil {
		t.Error("zero should return nil")
	}
}

func TestPtrFloat32OrNil_NonZero(t *testing.T) {
	p := ptrFloat32OrNil(9.8)
	if p == nil || *p != 9.8 {
		t.Errorf("expected 9.8, got %v", p)
	}
}

// --- ptrBoolOrNil ---

func TestPtrBoolOrNil_False(t *testing.T) {
	if ptrBoolOrNil(false) != nil {
		t.Error("false should return nil")
	}
}

func TestPtrBoolOrNil_True(t *testing.T) {
	p := ptrBoolOrNil(true)
	if p == nil || !*p {
		t.Error("expected pointer to true")
	}
}

// --- ptrTimeOrNil ---

func TestPtrTimeOrNil_Empty(t *testing.T) {
	if ptrTimeOrNil("") != nil {
		t.Error("empty string should return nil")
	}
}

func TestPtrTimeOrNil_ValidRFC3339(t *testing.T) {
	p := ptrTimeOrNil("2024-01-15T10:30:00Z")
	if p == nil {
		t.Fatal("expected non-nil time")
	}
	expected := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	if !p.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, *p)
	}
}

func TestPtrTimeOrNil_InvalidFormat(t *testing.T) {
	if ptrTimeOrNil("not-a-date") != nil {
		t.Error("invalid date should return nil")
	}
}

func TestPtrTimeOrNil_NonRFC3339(t *testing.T) {
	if ptrTimeOrNil("2024-01-15 10:30:00") != nil {
		t.Error("non-RFC3339 format should return nil")
	}
}

// --- toProtoSeverity ---

func TestToProtoSeverity_Empty(t *testing.T) {
	if toProtoSeverity("") != scannerv1.Severity_SEVERITY_UNSPECIFIED {
		t.Error("empty severity should return UNSPECIFIED")
	}
}

func TestToProtoSeverity_Critical(t *testing.T) {
	if toProtoSeverity(SeverityCritical) != scannerv1.Severity_SEVERITY_CRITICAL {
		t.Error("Critical severity mismatch")
	}
}

func TestToProtoSeverity_AllValues(t *testing.T) {
	tests := []struct {
		in   Severity
		want scannerv1.Severity
	}{
		{SeverityCritical, scannerv1.Severity_SEVERITY_CRITICAL},
		{SeverityHigh, scannerv1.Severity_SEVERITY_HIGH},
		{SeverityMedium, scannerv1.Severity_SEVERITY_MEDIUM},
		{SeverityLow, scannerv1.Severity_SEVERITY_LOW},
		{SeverityInfo, scannerv1.Severity_SEVERITY_INFO},
		{SeverityNone, scannerv1.Severity_SEVERITY_UNSPECIFIED},
	}
	for _, tc := range tests {
		got := toProtoSeverity(tc.in)
		if got != tc.want {
			t.Errorf("toProtoSeverity(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// --- toProtoDomains ---

func TestToProtoDomains_Empty(t *testing.T) {
	result := toProtoDomains(nil)
	if len(result) != 0 {
		t.Errorf("expected 0, got %d", len(result))
	}
}

func TestToProtoDomains_MergesAAndAAAA(t *testing.T) {
	domains := []Domain{
		{
			Domain: "example.com",
			A:      []string{"1.2.3.4"},
			AAAA:   []string{"::1"},
		},
	}
	result := toProtoDomains(domains)
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	ips := result[0].GetIp()
	if len(ips) != 2 {
		t.Fatalf("expected 2 merged IPs, got %d", len(ips))
	}
	if ips[0] != "1.2.3.4" || ips[1] != "::1" {
		t.Errorf("unexpected IPs: %v", ips)
	}
}

func TestToProtoDomains_OnlyA(t *testing.T) {
	result := toProtoDomains([]Domain{{Domain: "a.com", A: []string{"1.1.1.1"}}})
	if len(result[0].GetIp()) != 1 {
		t.Error("expected 1 IP from A record")
	}
}

func TestToProtoDomains_OnlyAAAA(t *testing.T) {
	result := toProtoDomains([]Domain{{Domain: "a.com", AAAA: []string{"::1"}}})
	if len(result[0].GetIp()) != 1 {
		t.Error("expected 1 IP from AAAA record")
	}
}

func TestToProtoDomains_NoIPs(t *testing.T) {
	result := toProtoDomains([]Domain{{Domain: "a.com"}})
	if len(result[0].GetIp()) != 0 {
		t.Error("no A/AAAA should yield empty Ip slice")
	}
}

func TestToProtoDomains_AllFields(t *testing.T) {
	result := toProtoDomains([]Domain{{
		Domain: "example.com",
		CNAME:  "alias.com",
		MX:     []string{"mx.example.com"},
		NS:     []string{"ns1.example.com"},
		TXT:    []string{"v=spf1"},
		SOA:    []string{"ns1.example.com hostmaster.example.com"},
		TTL:    300,
	}})
	r := result[0]
	if r.GetDomain() != "example.com" {
		t.Errorf("domain mismatch: got %q", r.GetDomain())
	}
	if r.GetCname() != "alias.com" {
		t.Errorf("cname mismatch: got %q", r.GetCname())
	}
	if r.GetTtl() != 300 {
		t.Errorf("ttl mismatch: got %d", r.GetTtl())
	}
}

func TestToProtoDomains_ZeroValueFields(t *testing.T) {
	result := toProtoDomains([]Domain{{}})
	r := result[0]
	if r.GetDomain() != "" {
		t.Error("empty domain should be empty string")
	}
	if r.GetCname() != "" {
		t.Error("empty cname should be empty string")
	}
	if r.GetTtl() != 0 {
		t.Error("zero ttl should be 0")
	}
}

// --- toProtoServices ---

func TestToProtoServices_Empty(t *testing.T) {
	result := toProtoServices(nil)
	if len(result) != 0 {
		t.Errorf("expected 0, got %d", len(result))
	}
}

func TestToProtoServices_BasicFields(t *testing.T) {
	result := toProtoServices([]Service{{
		Host:        "10.0.0.1",
		Port:        8080,
		ServiceName: "http-alt",
		CPEs:        []string{"cpe:/a:apache:httpd:2.4"},
	}})
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	s := result[0]
	if s.GetHost() != "10.0.0.1" {
		t.Errorf("host mismatch: %q", s.GetHost())
	}
	if s.GetPort() != 8080 {
		t.Errorf("port mismatch: %d", s.GetPort())
	}
	if s.GetServiceName() != "http-alt" {
		t.Errorf("service name mismatch: %q", s.GetServiceName())
	}
}

// --- toProtoWebFindings ---

func TestToProtoWebFindings_Empty(t *testing.T) {
	result := toProtoWebFindings(nil)
	if len(result) != 0 {
		t.Errorf("expected 0, got %d", len(result))
	}
}

func TestToProtoWebFindings_AllFields(t *testing.T) {
	findings := []WebFinding{{
		Name:        "SQL Injection",
		Description: "User input directly in query",
		Severity:    SeverityCritical,
		Endpoint:    "https://example.com/api/users",
		Category:    "injection",
		RuleID:      "sqli-001",
		CVE:         "CVE-2024-1234",
		CWEs:        []string{"CWE-89"},
		CVSSScore:   9.8,
		CVSSVector:  "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
		CurlCommand: "curl -X POST ...",
		References:  []string{"https://owasp.org/sqli"},
		Remediation: "Use parameterized queries",
		Requests: []HTTPRequest{
			{RawRequest: "POST /api", RawResponse: "500 Error"},
		},
	}}
	result := toProtoWebFindings(findings)
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	r := result[0]
	if r.GetName() != "SQL Injection" {
		t.Errorf("name mismatch: %q", r.GetName())
	}
	if r.GetCvssScore() != 9.8 {
		t.Errorf("CVSS score mismatch: %f", r.GetCvssScore())
	}
	if len(r.GetRequests()) != 1 {
		t.Errorf("requests mismatch: got %d", len(r.GetRequests()))
	}
	if r.GetSeverity() != scannerv1.Severity_SEVERITY_CRITICAL {
		t.Errorf("severity mismatch: %v", r.GetSeverity())
	}
}

// --- toProtoSASTFindings ---

func TestToProtoSASTFindings_Empty(t *testing.T) {
	result := toProtoSASTFindings(nil)
	if len(result) != 0 {
		t.Errorf("expected 0, got %d", len(result))
	}
}

func TestToProtoSASTFindings_WithCodeFlows(t *testing.T) {
	findings := []SASTFinding{{
		Name: "Taint Flow",
		File: "main.go",
		CodeFlows: []CodeFlowNode{
			{File: "a.go", StartLine: 1, EndLine: 1, Snippet: "input := r.URL.Query()"},
			{File: "b.go", StartLine: 5, EndLine: 5, Snippet: "db.Exec(input)"},
		},
	}}
	result := toProtoSASTFindings(findings)
	flows := result[0].GetCodeFlows()
	if len(flows) != 2 {
		t.Errorf("expected 2 flows, got %d", len(flows))
	}
}

func TestToProtoSASTFindings_WithCodeLines(t *testing.T) {
	findings := []SASTFinding{{
		Name: "Bug",
		CodeLines: []CodeLine{
			{Line: 10, Content: "exec(x)"},
		},
	}}
	result := toProtoSASTFindings(findings)
	lines := result[0].GetCodeLines()
	if len(lines) != 1 {
		t.Errorf("expected 1 line, got %d", len(lines))
	}
	if lines[0].GetLine() != 10 {
		t.Errorf("line number mismatch: %d", lines[0].GetLine())
	}
}

func TestToProtoSASTFindings_WithCommitSha(t *testing.T) {
	findings := []SASTFinding{{
		Name:      "Secret",
		CommitSha: "abc123",
	}}
	result := toProtoSASTFindings(findings)
	if result[0].GetCommitSha() != "abc123" {
		t.Errorf("commit SHA mismatch: %q", result[0].GetCommitSha())
	}
}

// --- toProtoCodeFlows ---

func TestToProtoCodeFlows_Empty(t *testing.T) {
	if toProtoCodeFlows(nil) != nil {
		t.Error("nil should return nil")
	}
	if toProtoCodeFlows([]CodeFlowNode{}) != nil {
		t.Error("empty slice should return nil")
	}
}

func TestToProtoCodeFlows_WithCodeLines(t *testing.T) {
	nodes := []CodeFlowNode{{
		File:      "a.go",
		StartLine: 1,
		CodeLines: []CodeLine{{Line: 1, Content: "x := 1"}},
	}}
	result := toProtoCodeFlows(nodes)
	if result == nil {
		t.Fatal("expected non-nil")
	}
	if len(result[0].GetCodeLines()) == 0 {
		t.Error("expected code lines on flow node")
	}
}

// --- toProtoCodeLines ---

func TestToProtoCodeLines_Empty(t *testing.T) {
	if toProtoCodeLines(nil) != nil {
		t.Error("nil should return nil")
	}
	if toProtoCodeLines([]CodeLine{}) != nil {
		t.Error("empty should return nil")
	}
}

func TestToProtoCodeLines_NonEmpty(t *testing.T) {
	result := toProtoCodeLines([]CodeLine{{Line: 42, Content: "return nil"}})
	if result == nil {
		t.Fatal("expected non-nil")
	}
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	if result[0].GetLine() != 42 {
		t.Errorf("line mismatch: %d", result[0].GetLine())
	}
}

// --- toProtoRawRequests ---

func TestToProtoRawRequests_Empty(t *testing.T) {
	if toProtoRawRequests(nil) != nil {
		t.Error("nil should return nil")
	}
	if toProtoRawRequests([]HTTPRequest{}) != nil {
		t.Error("empty should return nil")
	}
}

func TestToProtoRawRequests_NonEmpty(t *testing.T) {
	result := toProtoRawRequests([]HTTPRequest{
		{RawRequest: "GET / HTTP/1.1", RawResponse: "HTTP/1.1 200 OK"},
	})
	if result == nil {
		t.Fatal("expected non-nil")
	}
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	if result[0].GetRequest() != "GET / HTTP/1.1" {
		t.Errorf("request mismatch: %q", result[0].GetRequest())
	}
}
