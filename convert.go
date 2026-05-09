package rediver

import (
	"time"

	scannerv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/scanner/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// nilIfEmpty normalizes empty/nil Go slices to nil before assigning to proto
// repeated fields. Without this, a non-nil empty slice round-trips through
// the wire and downstream JSONB metadata as `[]`, polluting asset_metadata
// with empty arrays. nil signals "absent" — same shape as the REST/JSON
// ingress path.
func nilIfEmpty[T any](s []T) []T {
	if len(s) == 0 {
		return nil
	}
	return s
}

// toProtoDomains converts SDK Domain slice to proto DnsRecord slice.
func toProtoDomains(domains []Domain) []*scannerv1.DnsRecord {
	result := make([]*scannerv1.DnsRecord, len(domains))
	for i, d := range domains {
		// Merge A (IPv4) and AAAA (IPv6) into single ip field
		var ips []string
		ips = append(ips, d.A...)
		ips = append(ips, d.AAAA...)

		rec := &scannerv1.DnsRecord{
			Domain: d.Domain,
			Ip:     nilIfEmpty(ips),
			Mx:     nilIfEmpty(d.MX),
			Ns:     nilIfEmpty(d.NS),
			Txt:    nilIfEmpty(d.TXT),
			Soa:    nilIfEmpty(d.SOA),
		}
		if d.CNAME != "" {
			rec.Cname = &d.CNAME
		}
		if d.TTL != 0 {
			ttl := int32(d.TTL)
			rec.Ttl = &ttl
		}
		result[i] = rec
	}
	return result
}

// toProtoServices converts SDK Service slice to proto Service slice. The HTTP
// and Certificate sub-messages MUST be forwarded — without them
// scanner-emitted HTTP/TLS metadata is silently dropped before reaching the
// backend (status_code, title, webserver, technologies, certificate fields
// all become invisible).
func toProtoServices(services []Service) []*scannerv1.Service {
	result := make([]*scannerv1.Service, len(services))
	for i, s := range services {
		svc := &scannerv1.Service{
			Host: s.Host,
			Port: int32(s.Port),
			Cpes: nilIfEmpty(s.CPEs),
		}
		if s.ServiceName != "" {
			svc.ServiceName = &s.ServiceName
		}
		if s.HTTP != nil {
			svc.Http = toProtoHTTPInfo(s.HTTP)
		}
		if s.Certificate != nil {
			svc.Certificate = toProtoCertificate(s.Certificate)
		}
		result[i] = svc
	}
	return result
}

// toProtoHTTPInfo converts SDK HTTPInfo to proto HttpInfo.
func toProtoHTTPInfo(h *HTTPInfo) *scannerv1.HttpInfo {
	if h == nil {
		return nil
	}
	out := &scannerv1.HttpInfo{
		Ip:           nilIfEmpty(h.IPs),
		Technologies: nilIfEmpty(h.Technologies),
	}
	if h.URL != "" {
		out.Url = &h.URL
	}
	if h.Title != "" {
		out.Title = &h.Title
	}
	if h.StatusCode != 0 {
		sc := int32(h.StatusCode)
		out.StatusCode = &sc
	}
	if h.RedirectTo != "" {
		out.RedirectTo = &h.RedirectTo
	}
	if h.ContentType != "" {
		out.ContentType = &h.ContentType
	}
	if h.Webserver != "" {
		out.Webserver = &h.Webserver
	}
	if h.FaviconHash != "" {
		out.FaviconMmh3 = &h.FaviconHash
	}
	if h.ScreenshotURL != "" {
		out.ScreenshotUrl = &h.ScreenshotURL
	}
	if h.Scheme != "" {
		out.Scheme = &h.Scheme
	}
	if h.Path != "" {
		out.Path = &h.Path
	}
	return out
}

// toProtoCertificate converts SDK TLSInfo to proto Certificate. NotBefore /
// NotAfter are parsed as RFC3339 — invalid strings produce nil timestamps
// rather than failing the whole record.
func toProtoCertificate(c *TLSInfo) *scannerv1.Certificate {
	if c == nil {
		return nil
	}
	out := &scannerv1.Certificate{
		SubjectAn: nilIfEmpty(c.SubjectAltNames),
	}
	if c.Fingerprint != "" {
		out.Fingerprint = &c.Fingerprint
	}
	if c.Serial != "" {
		out.Serial = &c.Serial
	}
	if c.SubjectCN != "" {
		out.SubjectCn = &c.SubjectCN
	}
	if c.SubjectOrg != "" {
		out.SubjectOrg = &c.SubjectOrg
	}
	if c.IssuerCN != "" {
		out.IssuerCn = &c.IssuerCN
	}
	if c.IssuerOrg != "" {
		out.IssuerOrg = &c.IssuerOrg
	}
	if t := parseRFC3339(c.NotBefore); t != nil {
		out.NotBefore = timestamppb.New(*t)
	}
	if t := parseRFC3339(c.NotAfter); t != nil {
		out.NotAfter = timestamppb.New(*t)
	}
	if c.IsWildcard {
		out.Wildcard = &c.IsWildcard
	}
	return out
}

func parseRFC3339(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}

// toProtoWebFindings converts SDK WebFinding slice to proto WebFinding slice.
func toProtoWebFindings(findings []WebFinding) []*scannerv1.WebFinding {
	result := make([]*scannerv1.WebFinding, len(findings))
	for i, f := range findings {
		wf := &scannerv1.WebFinding{
			Name:       f.Name,
			Severity:   toProtoSeverity(f.Severity),
			Endpoint:   f.Endpoint,
			Cwes:       nilIfEmpty(f.CWEs),
			References: nilIfEmpty(f.References),
		}
		if f.Description != "" {
			wf.Description = &f.Description
		}
		if f.Category != "" {
			wf.Category = &f.Category
		}
		if f.Remediation != "" {
			wf.Remediation = &f.Remediation
		}
		if f.CVE != "" {
			wf.Cve = &f.CVE
		}
		if f.CVSSVector != "" {
			wf.CvssVector = &f.CVSSVector
		}
		if f.CVSSScore != 0 {
			wf.CvssScore = &f.CVSSScore
		}
		if f.RuleID != "" {
			wf.RuleId = &f.RuleID
		}
		if f.CurlCommand != "" {
			wf.CurlCommand = &f.CurlCommand
		}
		wf.Requests = toProtoRawRequests(f.Requests)
		result[i] = wf
	}
	return result
}

// toProtoSASTFindings converts SDK SASTFinding slice to proto CodeFinding slice.
func toProtoSASTFindings(findings []SASTFinding) []*scannerv1.CodeFinding {
	result := make([]*scannerv1.CodeFinding, len(findings))
	for i, f := range findings {
		cf := &scannerv1.CodeFinding{
			Name:       f.Name,
			Severity:   toProtoSeverity(f.Severity),
			File:       f.File,
			Cwes:       nilIfEmpty(f.CWEs),
			References: nilIfEmpty(f.References),
			CodeFlows:  toProtoCodeFlows(f.CodeFlows),
			CodeLines:  toProtoCodeLines(f.CodeLines),
		}
		if f.Description != "" {
			cf.Description = &f.Description
		}
		if f.Category != "" {
			cf.Category = &f.Category
		}
		if f.Remediation != "" {
			cf.Remediation = &f.Remediation
		}
		if f.CVE != "" {
			cf.Cve = &f.CVE
		}
		if f.CVSSVector != "" {
			cf.CvssVector = &f.CVSSVector
		}
		if f.CVSSScore != 0 {
			cf.CvssScore = &f.CVSSScore
		}
		if f.RuleID != "" {
			cf.RuleId = &f.RuleID
		}
		if f.Snippet != "" {
			cf.Snippet = &f.Snippet
		}
		if f.StartLine != 0 {
			sl := int32(f.StartLine)
			cf.StartLine = &sl
		}
		if f.EndLine != 0 {
			el := int32(f.EndLine)
			cf.EndLine = &el
		}
		if f.CommitSha != "" {
			cf.CommitSha = &f.CommitSha
		}
		result[i] = cf
	}
	return result
}

// toProtoCodeFlows converts SDK CodeFlowNode slice to proto CodeFlowNode slice.
func toProtoCodeFlows(nodes []CodeFlowNode) []*scannerv1.CodeFlowNode {
	if len(nodes) == 0 {
		return nil
	}
	result := make([]*scannerv1.CodeFlowNode, len(nodes))
	for i, n := range nodes {
		node := &scannerv1.CodeFlowNode{
			File:      n.File,
			Snippet:   n.Snippet,
			StartLine: int32(n.StartLine),
			CodeLines: toProtoCodeLines(n.CodeLines),
		}
		if n.EndLine != 0 {
			el := int32(n.EndLine)
			node.EndLine = &el
		}
		if n.StartColumn != 0 {
			sc := int32(n.StartColumn)
			node.StartColumn = &sc
		}
		if n.EndColumn != 0 {
			ec := int32(n.EndColumn)
			node.EndColumn = &ec
		}
		if n.Message != "" {
			node.Message = &n.Message
		}
		result[i] = node
	}
	return result
}

// toProtoCodeLines converts SDK CodeLine slice to proto CodeLine slice.
func toProtoCodeLines(lines []CodeLine) []*scannerv1.CodeLine {
	if len(lines) == 0 {
		return nil
	}
	result := make([]*scannerv1.CodeLine, len(lines))
	for i, l := range lines {
		result[i] = &scannerv1.CodeLine{
			Line:    int32(l.Line),
			Content: l.Content,
		}
	}
	return result
}

// toProtoRawRequests converts SDK HTTPRequest slice to proto RawRequest slice.
func toProtoRawRequests(requests []HTTPRequest) []*scannerv1.RawRequest {
	if len(requests) == 0 {
		return nil
	}
	result := make([]*scannerv1.RawRequest, len(requests))
	for i, r := range requests {
		ri := &scannerv1.RawRequest{}
		if r.RawRequest != "" {
			ri.Request = &r.RawRequest
		}
		if r.RawResponse != "" {
			ri.Response = &r.RawResponse
		}
		result[i] = ri
	}
	return result
}

// Helper functions for pointer conversion

func ptrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func ptrSliceOrNil(s []string) *[]string {
	if len(s) == 0 {
		return nil
	}
	return &s
}

func ptrInt32OrNil(i int) *int32 {
	if i == 0 {
		return nil
	}
	v := int32(i)
	return &v
}

func ptrFloat32OrNil(f float32) *float32 {
	if f == 0 {
		return nil
	}
	return &f
}

func ptrBoolOrNil(b bool) *bool {
	if !b {
		return nil
	}
	return &b
}

func ptrTimeOrNil(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}
