package rediver

import (
	"testing"
	"time"
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
	// ISO 8601 but not RFC3339 (missing T separator or timezone)
	if ptrTimeOrNil("2024-01-15 10:30:00") != nil {
		t.Error("non-RFC3339 format should return nil")
	}
}

// --- toAPISeverity ---

func TestToAPISeverity_Empty(t *testing.T) {
	if toAPISeverity("") != nil {
		t.Error("empty severity should return nil")
	}
}

func TestToAPISeverity_Valid(t *testing.T) {
	p := toAPISeverity(SeverityCritical)
	if p == nil {
		t.Fatal("expected non-nil severity")
	}
}

// --- toAPIDomains ---

func TestToAPIDomains_Empty(t *testing.T) {
	result := toAPIDomains(nil)
	if len(result) != 0 {
		t.Errorf("expected 0, got %d", len(result))
	}
}

func TestToAPIDomains_MergesAAndAAAA(t *testing.T) {
	domains := []Domain{
		{
			Domain: "example.com",
			A:      []string{"1.2.3.4"},
			AAAA:   []string{"::1"},
		},
	}
	result := toAPIDomains(domains)
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	if result[0].Ip == nil {
		t.Fatal("expected merged IPs")
	}
	ips := *result[0].Ip
	if len(ips) != 2 {
		t.Fatalf("expected 2 merged IPs, got %d", len(ips))
	}
	if ips[0] != "1.2.3.4" || ips[1] != "::1" {
		t.Errorf("unexpected IPs: %v", ips)
	}
}

func TestToAPIDomains_OnlyA(t *testing.T) {
	result := toAPIDomains([]Domain{{Domain: "a.com", A: []string{"1.1.1.1"}}})
	if result[0].Ip == nil || len(*result[0].Ip) != 1 {
		t.Error("expected 1 IP from A record")
	}
}

func TestToAPIDomains_OnlyAAAA(t *testing.T) {
	result := toAPIDomains([]Domain{{Domain: "a.com", AAAA: []string{"::1"}}})
	if result[0].Ip == nil || len(*result[0].Ip) != 1 {
		t.Error("expected 1 IP from AAAA record")
	}
}

func TestToAPIDomains_NoIPs(t *testing.T) {
	result := toAPIDomains([]Domain{{Domain: "a.com"}})
	if result[0].Ip != nil {
		t.Error("no A/AAAA should yield nil Ip")
	}
}

func TestToAPIDomains_AllFields(t *testing.T) {
	result := toAPIDomains([]Domain{{
		Domain: "example.com",
		CNAME:  "alias.com",
		MX:     []string{"mx.example.com"},
		NS:     []string{"ns1.example.com"},
		TXT:    []string{"v=spf1"},
		SOA:    []string{"ns1.example.com hostmaster.example.com"},
		TTL:    300,
	}})
	r := result[0]
	if r.Domain == nil || *r.Domain != "example.com" {
		t.Error("domain mismatch")
	}
	if r.Cname == nil || *r.Cname != "alias.com" {
		t.Error("cname mismatch")
	}
	if r.Ttl == nil || *r.Ttl != 300 {
		t.Error("ttl mismatch")
	}
}

func TestToAPIDomains_ZeroValueFields(t *testing.T) {
	result := toAPIDomains([]Domain{{}})
	r := result[0]
	// All zero-value fields should map to nil pointers
	if r.Domain != nil {
		t.Error("empty domain should be nil")
	}
	if r.Cname != nil {
		t.Error("empty cname should be nil")
	}
	if r.Ttl != nil {
		t.Error("zero ttl should be nil")
	}
}

// --- toAPIServices ---

func TestToAPIServices_Empty(t *testing.T) {
	result := toAPIServices(nil)
	if len(result) != 0 {
		t.Errorf("expected 0, got %d", len(result))
	}
}

func TestToAPIServices_BasicFields(t *testing.T) {
	result := toAPIServices([]Service{{
		Host:        "10.0.0.1",
		Port:        8080,
		ServiceName: "http-alt",
		CPEs:        []string{"cpe:/a:apache:httpd:2.4"},
	}})
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	s := result[0]
	if s.Host == nil || *s.Host != "10.0.0.1" {
		t.Error("host mismatch")
	}
	if s.Port == nil || *s.Port != 8080 {
		t.Error("port mismatch")
	}
	if s.Http != nil {
		t.Error("no HTTP info should yield nil")
	}
	if s.Certificate != nil {
		t.Error("no cert should yield nil")
	}
}

func TestToAPIServices_WithHTTP(t *testing.T) {
	result := toAPIServices([]Service{{
		Host: "example.com",
		Port: 443,
		HTTP: &HTTPInfo{
			URL:           "https://example.com",
			Scheme:        "https",
			StatusCode:    200,
			Title:         "Example",
			Technologies:  []string{"nginx"},
			IPs:           []string{"1.2.3.4"},
		},
	}})
	s := result[0]
	if s.Http == nil {
		t.Fatal("expected HTTP info")
	}
	if s.Http.Url == nil || *s.Http.Url != "https://example.com" {
		t.Error("URL mismatch")
	}
	if s.Http.StatusCode == nil || *s.Http.StatusCode != 200 {
		t.Error("status code mismatch")
	}
}

func TestToAPIServices_WithCertificate(t *testing.T) {
	result := toAPIServices([]Service{{
		Host: "example.com",
		Port: 443,
		Certificate: &TLSInfo{
			SubjectCN:      "*.example.com",
			IssuerCN:       "Let's Encrypt",
			IsWildcard:     true,
			NotBefore:      "2024-01-01T00:00:00Z",
			NotAfter:       "2025-01-01T00:00:00Z",
			SubjectAltNames: []string{"example.com", "*.example.com"},
		},
	}})
	s := result[0]
	if s.Certificate == nil {
		t.Fatal("expected certificate info")
	}
	if s.Certificate.SubjectCn == nil || *s.Certificate.SubjectCn != "*.example.com" {
		t.Error("subject CN mismatch")
	}
	if s.Certificate.Wildcard == nil || !*s.Certificate.Wildcard {
		t.Error("wildcard should be true")
	}
	if s.Certificate.NotBefore == nil {
		t.Error("expected NotBefore time")
	}
}

// --- toAPIHttpInfo ---

func TestToAPIHttpInfo_Nil(t *testing.T) {
	if toAPIHttpInfo(nil) != nil {
		t.Error("nil should return nil")
	}
}

func TestToAPIHttpInfo_AllFields(t *testing.T) {
	h := &HTTPInfo{
		URL:           "https://example.com/path",
		Scheme:        "https",
		Path:          "/path",
		StatusCode:    301,
		Title:         "Redirect",
		ContentType:   "text/html",
		Webserver:     "nginx",
		Technologies:  []string{"React", "Node.js"},
		RedirectTo:    "https://www.example.com",
		FaviconHash:   "abc123",
		ScreenshotURL: "https://screenshots.example.com/1.png",
		IPs:           []string{"1.2.3.4", "5.6.7.8"},
	}
	result := toAPIHttpInfo(h)
	if result == nil {
		t.Fatal("expected non-nil")
	}
	if result.Url == nil || *result.Url != h.URL {
		t.Error("URL mismatch")
	}
	if result.RedirectTo == nil || *result.RedirectTo != h.RedirectTo {
		t.Error("redirect mismatch")
	}
	if result.Technologies == nil || len(*result.Technologies) != 2 {
		t.Error("technologies mismatch")
	}
}

func TestToAPIHttpInfo_ZeroValues(t *testing.T) {
	result := toAPIHttpInfo(&HTTPInfo{})
	if result == nil {
		t.Fatal("should return non-nil for empty HTTPInfo")
	}
	// All zero-value fields should yield nil
	if result.Url != nil {
		t.Error("empty URL should be nil")
	}
	if result.StatusCode != nil {
		t.Error("zero status code should be nil")
	}
}

// --- toAPICertificateInfo ---

func TestToAPICertificateInfo_Nil(t *testing.T) {
	if toAPICertificateInfo(nil) != nil {
		t.Error("nil should return nil")
	}
}

func TestToAPICertificateInfo_InvalidTime(t *testing.T) {
	result := toAPICertificateInfo(&TLSInfo{
		NotBefore: "invalid-date",
		NotAfter:  "also-invalid",
	})
	if result.NotBefore != nil {
		t.Error("invalid NotBefore should be nil")
	}
	if result.NotAfter != nil {
		t.Error("invalid NotAfter should be nil")
	}
}

// --- toAPIWebFindings ---

func TestToAPIWebFindings_Empty(t *testing.T) {
	result := toAPIWebFindings(nil)
	if len(result) != 0 {
		t.Errorf("expected 0, got %d", len(result))
	}
}

func TestToAPIWebFindings_AllFields(t *testing.T) {
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
	result := toAPIWebFindings(findings)
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	r := result[0]
	if r.Name == nil || *r.Name != "SQL Injection" {
		t.Error("name mismatch")
	}
	if r.CvssScore == nil || *r.CvssScore != 9.8 {
		t.Error("CVSS score mismatch")
	}
	if r.Requests == nil || len(*r.Requests) != 1 {
		t.Error("requests mismatch")
	}
}

// --- toAPISASTFindings ---

func TestToAPISASTFindings_Empty(t *testing.T) {
	result := toAPISASTFindings(nil)
	if len(result) != 0 {
		t.Errorf("expected 0, got %d", len(result))
	}
}

func TestToAPISASTFindings_WithCodeFlows(t *testing.T) {
	findings := []SASTFinding{{
		Name:     "Taint Flow",
		File:     "main.go",
		CodeFlows: []CodeFlowNode{
			{File: "a.go", StartLine: 1, EndLine: 1, Snippet: "input := r.URL.Query()"},
			{File: "b.go", StartLine: 5, EndLine: 5, Snippet: "db.Exec(input)"},
		},
	}}
	result := toAPISASTFindings(findings)
	if result[0].CodeFlows == nil {
		t.Fatal("expected code flows")
	}
	flows := *result[0].CodeFlows
	if len(flows) != 2 {
		t.Errorf("expected 2 flows, got %d", len(flows))
	}
}

func TestToAPISASTFindings_WithCodeLines(t *testing.T) {
	findings := []SASTFinding{{
		Name: "Bug",
		CodeLines: []CodeLine{
			{Line: 10, Content: "exec(x)"},
		},
	}}
	result := toAPISASTFindings(findings)
	if result[0].CodeLines == nil {
		t.Fatal("expected code lines")
	}
	lines := *result[0].CodeLines
	if len(lines) != 1 {
		t.Errorf("expected 1 line, got %d", len(lines))
	}
}

func TestToAPISASTFindings_WithCommitSha(t *testing.T) {
	findings := []SASTFinding{{
		Name:      "Secret",
		CommitSha: "abc123",
	}}
	result := toAPISASTFindings(findings)
	if result[0].CommitSha == nil || *result[0].CommitSha != "abc123" {
		t.Error("commit SHA mismatch")
	}
}

// --- toAPICodeFlows ---

func TestToAPICodeFlows_Empty(t *testing.T) {
	if toAPICodeFlows(nil) != nil {
		t.Error("nil should return nil")
	}
	if toAPICodeFlows([]CodeFlowNode{}) != nil {
		t.Error("empty slice should return nil")
	}
}

func TestToAPICodeFlows_WithCodeLines(t *testing.T) {
	nodes := []CodeFlowNode{{
		File:      "a.go",
		StartLine: 1,
		CodeLines: []CodeLine{{Line: 1, Content: "x := 1"}},
	}}
	result := toAPICodeFlows(nodes)
	if result == nil {
		t.Fatal("expected non-nil")
	}
	if (*result)[0].CodeLines == nil {
		t.Error("expected code lines on flow node")
	}
}

// --- toAPICodeLines ---

func TestToAPICodeLines_Empty(t *testing.T) {
	if toAPICodeLines(nil) != nil {
		t.Error("nil should return nil")
	}
	if toAPICodeLines([]CodeLine{}) != nil {
		t.Error("empty should return nil")
	}
}

func TestToAPICodeLines_NonEmpty(t *testing.T) {
	result := toAPICodeLines([]CodeLine{{Line: 42, Content: "return nil"}})
	if result == nil {
		t.Fatal("expected non-nil")
	}
	lines := *result
	if len(lines) != 1 {
		t.Fatalf("expected 1, got %d", len(lines))
	}
}

// --- toAPIRawRequests ---

func TestToAPIRawRequests_Empty(t *testing.T) {
	if toAPIRawRequests(nil) != nil {
		t.Error("nil should return nil")
	}
	if toAPIRawRequests([]HTTPRequest{}) != nil {
		t.Error("empty should return nil")
	}
}

func TestToAPIRawRequests_NonEmpty(t *testing.T) {
	result := toAPIRawRequests([]HTTPRequest{
		{RawRequest: "GET / HTTP/1.1", RawResponse: "HTTP/1.1 200 OK"},
	})
	if result == nil {
		t.Fatal("expected non-nil")
	}
	reqs := *result
	if len(reqs) != 1 {
		t.Fatalf("expected 1, got %d", len(reqs))
	}
	if reqs[0].Request == nil || *reqs[0].Request != "GET / HTTP/1.1" {
		t.Error("request mismatch")
	}
}
