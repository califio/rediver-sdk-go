package rediver

// Result represents scan output that will be imported to Rediver.
// Use the helper functions Domains(), Services(), Findings() to create results.
type Result struct {
	domains  []Domain
	services []Service
	findings []finding
}

// finding is an internal wrapper for different finding types.
type finding struct {
	web  *WebFinding
	sast *SASTFinding
}

// Domains creates a Result containing domain records.
func Domains(domains ...Domain) Result {
	return Result{domains: domains}
}

// Services creates a Result containing service records.
func Services(services ...Service) Result {
	return Result{services: services}
}

// WebFindings creates a Result containing web security findings.
func WebFindings(findings ...WebFinding) Result {
	result := Result{}
	for i := range findings {
		result.findings = append(result.findings, finding{web: &findings[i]})
	}
	return result
}

// SASTFindings creates a Result containing SAST/code security findings.
func SASTFindings(findings ...SASTFinding) Result {
	result := Result{}
	for i := range findings {
		result.findings = append(result.findings, finding{sast: &findings[i]})
	}
	return result
}

// GetDomains returns the domain records in this result.
func (r Result) GetDomains() []Domain {
	return r.domains
}

// GetServices returns the service records in this result.
func (r Result) GetServices() []Service {
	return r.services
}

// GetWebFindings returns the web findings in this result.
func (r Result) GetWebFindings() []WebFinding {
	var result []WebFinding
	for _, f := range r.findings {
		if f.web != nil {
			result = append(result, *f.web)
		}
	}
	return result
}

// GetSASTFindings returns the SAST/code findings in this result.
func (r Result) GetSASTFindings() []SASTFinding {
	var result []SASTFinding
	for _, f := range r.findings {
		if f.sast != nil {
			result = append(result, *f.sast)
		}
	}
	return result
}

// HasData returns true if the result contains any data.
func (r Result) HasData() bool {
	return len(r.domains) > 0 || len(r.services) > 0 || len(r.findings) > 0
}

// ----------------------------------------------------------------------------
// Domain Types
// ----------------------------------------------------------------------------

// Domain represents a discovered DNS record.
type Domain struct {
	// Domain is the fully qualified domain name (e.g., "api.example.com")
	Domain string

	// A contains IPv4 addresses
	A []string

	// AAAA contains IPv6 addresses
	AAAA []string

	// CNAME is the canonical name if this is a CNAME record
	CNAME string

	// MX contains mail exchange records
	MX []string

	// TXT contains text records
	TXT []string

	// SOA contains start of authority records
	SOA []string

	// NS contains name server records
	NS []string

	// TTL is the time-to-live in seconds
	TTL int
}

// ----------------------------------------------------------------------------
// Service Types
// ----------------------------------------------------------------------------

// Service represents a discovered network service.
type Service struct {
	// Host is the hostname or IP address
	Host string

	// Port is the service port number
	Port int

	// ServiceName is the detected service type (e.g., "http", "ssh", "mysql")
	ServiceName string

	// ServiceStatus indicates if the port is open/closed/filtered
	ServiceStatus string

	// CPEs are Common Platform Enumeration identifiers
	CPEs []string

	// Certificate contains TLS certificate information (for any TLS-enabled service)
	Certificate *TLSInfo

	// HTTP contains website-specific information (only for HTTP/HTTPS services)
	HTTP *HTTPInfo
}

// HTTPInfo contains website-specific information for HTTP services.
type HTTPInfo struct {
	// URL is the full URL (e.g., "https://example.com:8443/path")
	URL string

	// Scheme is the protocol (http or https)
	Scheme string

	// Path is the URL path
	Path string

	// StatusCode is the HTTP response status code
	StatusCode int

	// Title is the HTML page title
	Title string

	// ContentType is the Content-Type header value
	ContentType string

	// Webserver is the detected web server (e.g., "nginx", "Apache")
	Webserver string

	// Technologies are detected web technologies
	Technologies []string

	// RedirectTo is the redirect target URL if any
	RedirectTo string

	// FaviconHash is the MurmurHash3 of the favicon
	FaviconHash string

	// ScreenshotURL is the URL to a screenshot of the page
	ScreenshotURL string

	// IPs are resolved IP addresses
	IPs []string
}

// TLSInfo contains TLS/SSL certificate information.
type TLSInfo struct {
	// SubjectCN is the certificate subject common name
	SubjectCN string

	// SubjectOrg is the certificate subject organization
	SubjectOrg string

	// SubjectAltNames are subject alternative names
	SubjectAltNames []string

	// IssuerCN is the issuer common name
	IssuerCN string

	// IssuerOrg is the issuer organization
	IssuerOrg string

	// Serial is the certificate serial number
	Serial string

	// Fingerprint is the certificate fingerprint
	Fingerprint string

	// NotBefore is when the certificate becomes valid
	NotBefore string

	// NotAfter is when the certificate expires
	NotAfter string

	// IsWildcard indicates if this is a wildcard certificate
	IsWildcard bool
}

// ----------------------------------------------------------------------------
// Finding Types
// ----------------------------------------------------------------------------

// WebFinding represents a web security finding.
type WebFinding struct {
	// Name is the finding title (e.g., "SQL Injection", "XSS")
	Name string

	// Description explains the vulnerability
	Description string

	// Severity is the finding severity level
	Severity Severity

	// Endpoint is the vulnerable URL
	Endpoint string

	// Category is the finding category
	Category string

	// RuleID is the scanner rule that detected this
	RuleID string

	// CVE is the CVE identifier if applicable
	CVE string

	// CWEs are CWE identifiers
	CWEs []string

	// CVSSScore is the CVSS score (0.0-10.0)
	CVSSScore float32

	// CVSSVector is the CVSS vector string
	CVSSVector string

	// CurlCommand is a curl command to reproduce the issue
	CurlCommand string

	// References are URLs with more information
	References []string

	// Remediation describes how to fix the issue
	Remediation string

	// Requests contains multiple request/response pairs for multi-step findings
	Requests []HTTPRequest
}

// HTTPRequest represents an HTTP request/response pair.
type HTTPRequest struct {
	RawRequest  string
	RawResponse string
}

// SASTFinding represents a SAST (Static Application Security Testing) finding.
type SASTFinding struct {
	// Name is the finding title
	Name string

	// Description explains the vulnerability
	Description string

	// Severity is the finding severity level
	Severity Severity

	// File is the path to the vulnerable file
	File string

	// StartLine is the starting line number
	StartLine int

	// EndLine is the ending line number
	EndLine int

	// Snippet is the vulnerable code snippet
	Snippet string

	// Category is the finding category (e.g., "secret", "sql-injection")
	Category string

	// RuleID is the scanner rule that detected this
	RuleID string

	// CVE is the CVE identifier if applicable
	CVE string

	// CWEs are CWE identifiers
	CWEs []string

	// CVSSScore is the CVSS score (0.0-10.0)
	CVSSScore float32

	// CVSSVector is the CVSS vector string
	CVSSVector string

	// References are URLs with more information
	References []string

	// Remediation describes how to fix the issue
	Remediation string

	// CodeFlows shows the data flow path for taint analysis
	CodeFlows []CodeFlowNode

	// CodeLines provides surrounding code context for display.
	// Each entry is a single line with its absolute line number.
	// Non-contiguous lines are supported (gaps rendered as "···" in UI).
	CodeLines []CodeLine

	// CommitSha is the git commit where this finding was introduced (optional).
	// When set, overrides the job-level HEAD commit SHA for this specific finding.
	CommitSha string
}

// CodeFlowNode represents a step in a code flow/taint analysis.
type CodeFlowNode struct {
	// File is the source file path
	File string

	// StartLine is the starting line number
	StartLine int

	// EndLine is the ending line number
	EndLine int

	// StartColumn is the starting column
	StartColumn int

	// EndColumn is the ending column
	EndColumn int

	// Snippet is the code at this location
	Snippet string

	// Message describes what happens at this step
	Message string

	// CodeLines provides surrounding code context for this flow node
	CodeLines []CodeLine
}

// CodeLine represents a single line of code context with its line number.
type CodeLine struct {
	// Line is the absolute line number in the source file
	Line int

	// Content is the text content of this line
	Content string
}
