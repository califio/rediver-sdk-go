package rediver

import (
	"time"

	"github.com/califio/rediver-sdk-go/internal/api"
)

// toAPIDomains converts SDK Domain slice to API DnsRecord slice.
func toAPIDomains(domains []Domain) []api.DnsRecord {
	result := make([]api.DnsRecord, len(domains))
	for i, d := range domains {
		// Merge A (IPv4) and AAAA (IPv6) into single Ip field
		var ips []string
		ips = append(ips, d.A...)
		ips = append(ips, d.AAAA...)

		result[i] = api.DnsRecord{
			Domain: ptrOrNil(d.Domain),
			Ip:     ptrSliceOrNil(ips),
			Cname:  ptrOrNil(d.CNAME),
			Mx:     ptrSliceOrNil(d.MX),
			Ns:     ptrSliceOrNil(d.NS),
			Txt:    ptrSliceOrNil(d.TXT),
			Soa:    ptrSliceOrNil(d.SOA),
			Ttl:    ptrInt32OrNil(d.TTL),
		}
	}
	return result
}

// toAPIServices converts SDK Service slice to API Service slice.
func toAPIServices(services []Service) []api.Service {
	result := make([]api.Service, len(services))
	for i, s := range services {
		result[i] = api.Service{
			Host:        ptrOrNil(s.Host),
			Port:        ptrInt32OrNil(s.Port),
			ServiceName: ptrOrNil(s.ServiceName),
			Cpes:        ptrSliceOrNil(s.CPEs),
		}
		if s.HTTP != nil {
			result[i].Http = toAPIHttpInfo(s.HTTP)
		}
		if s.Certificate != nil {
			result[i].Certificate = toAPICertificateInfo(s.Certificate)
		}
	}
	return result
}

// toAPIHttpInfo converts SDK HTTPInfo to API HttpInfo.
func toAPIHttpInfo(h *HTTPInfo) *api.HttpInfo {
	if h == nil {
		return nil
	}
	return &api.HttpInfo{
		Url:           ptrOrNil(h.URL),
		Scheme:        ptrOrNil(h.Scheme),
		Path:          ptrOrNil(h.Path),
		StatusCode:    ptrInt32OrNil(h.StatusCode),
		Title:         ptrOrNil(h.Title),
		ContentType:   ptrOrNil(h.ContentType),
		Webserver:     ptrOrNil(h.Webserver),
		Technologies:  ptrSliceOrNil(h.Technologies),
		RedirectTo:    ptrOrNil(h.RedirectTo),
		FaviconMmh3:   ptrOrNil(h.FaviconHash),
		ScreenshotUrl: ptrOrNil(h.ScreenshotURL),
		Ip:            ptrSliceOrNil(h.IPs),
	}
}

// toAPICertificateInfo converts SDK TLSInfo to API CertificateInfo.
func toAPICertificateInfo(t *TLSInfo) *api.CertificateInfo {
	if t == nil {
		return nil
	}
	return &api.CertificateInfo{
		SubjectCn:   ptrOrNil(t.SubjectCN),
		SubjectOrg:  ptrOrNil(t.SubjectOrg),
		SubjectAn:   ptrSliceOrNil(t.SubjectAltNames),
		IssuerCn:    ptrOrNil(t.IssuerCN),
		IssuerOrg:   ptrOrNil(t.IssuerOrg),
		Serial:      ptrOrNil(t.Serial),
		Fingerprint: ptrOrNil(t.Fingerprint),
		Wildcard:    ptrBoolOrNil(t.IsWildcard),
		NotBefore:   ptrTimeOrNil(t.NotBefore),
		NotAfter:    ptrTimeOrNil(t.NotAfter),
	}
}

// toAPIWebFindings converts SDK WebFinding slice to API WebFinding slice.
func toAPIWebFindings(findings []WebFinding) []api.WebFinding {
	result := make([]api.WebFinding, len(findings))
	for i, f := range findings {
		result[i] = api.WebFinding{
			Name:        ptrOrNil(f.Name),
			Description: ptrOrNil(f.Description),
			Severity:    toAPISeverity(f.Severity),
			Endpoint:    ptrOrNil(f.Endpoint),
			Category:    ptrOrNil(f.Category),
			RuleId:      ptrOrNil(f.RuleID),
			Cve:         ptrOrNil(f.CVE),
			Cwes:        ptrSliceOrNil(f.CWEs),
			CvssScore:   ptrFloat32OrNil(f.CVSSScore),
			CvssVector:  ptrOrNil(f.CVSSVector),
			CurlCommand: ptrOrNil(f.CurlCommand),
			References:  ptrSliceOrNil(f.References),
			Remediation: ptrOrNil(f.Remediation),
			Requests:    toAPIRawRequests(f.Requests),
		}
	}
	return result
}

// toAPISASTFindings converts SDK SASTFinding slice to API CodeFinding slice.
func toAPISASTFindings(findings []SASTFinding) []api.CodeFinding {
	result := make([]api.CodeFinding, len(findings))
	for i, f := range findings {
		result[i] = api.CodeFinding{
			Name:        ptrOrNil(f.Name),
			Description: ptrOrNil(f.Description),
			Severity:    toAPISeverity(f.Severity),
			File:        ptrOrNil(f.File),
			StartLine:   ptrInt32OrNil(f.StartLine),
			EndLine:     ptrInt32OrNil(f.EndLine),
			Snippet:     ptrOrNil(f.Snippet),
			Category:    ptrOrNil(f.Category),
			RuleId:      ptrOrNil(f.RuleID),
			Cve:         ptrOrNil(f.CVE),
			Cwes:        ptrSliceOrNil(f.CWEs),
			CvssScore:   ptrFloat32OrNil(f.CVSSScore),
			CvssVector:  ptrOrNil(f.CVSSVector),
			References:  ptrSliceOrNil(f.References),
			Remediation: ptrOrNil(f.Remediation),
			CodeFlows:   toAPICodeFlows(f.CodeFlows),
			CodeLines:   toAPICodeLines(f.CodeLines),
			CommitSha:   ptrOrNil(f.CommitSha),
		}
	}
	return result
}

// toAPICodeFlows converts SDK CodeFlowNode slice to API CodeFlowNode slice.
func toAPICodeFlows(nodes []CodeFlowNode) *[]api.CodeFlowNode {
	if len(nodes) == 0 {
		return nil
	}
	result := make([]api.CodeFlowNode, len(nodes))
	for i, n := range nodes {
		result[i] = api.CodeFlowNode{
			File:        ptrOrNil(n.File),
			StartLine:   ptrInt32OrNil(n.StartLine),
			EndLine:     ptrInt32OrNil(n.EndLine),
			StartColumn: ptrInt32OrNil(n.StartColumn),
			EndColumn:   ptrInt32OrNil(n.EndColumn),
			Snippet:     ptrOrNil(n.Snippet),
			Message:     ptrOrNil(n.Message),
			CodeLines:   toAPICodeLines(n.CodeLines),
		}
	}
	return &result
}

// toAPICodeLines converts SDK CodeLine slice to API CodeLine slice.
func toAPICodeLines(lines []CodeLine) *[]api.CodeLine {
	if len(lines) == 0 {
		return nil
	}
	result := make([]api.CodeLine, len(lines))
	for i, l := range lines {
		result[i] = api.CodeLine{
			Line:    ptrInt32OrNil(l.Line),
			Content: ptrOrNil(l.Content),
		}
	}
	return &result
}

// toAPIRawRequests converts SDK HTTPRequest slice to API RawRequest slice.
func toAPIRawRequests(requests []HTTPRequest) *[]api.RawRequest {
	if len(requests) == 0 {
		return nil
	}
	result := make([]api.RawRequest, len(requests))
	for i, r := range requests {
		result[i] = api.RawRequest{
			Request:  ptrOrNil(r.RawRequest),
			Response: ptrOrNil(r.RawResponse),
		}
	}
	return &result
}

// toAPISeverity converts SDK Severity to API Severity pointer.
func toAPISeverity(s Severity) *api.Severity {
	if s == "" {
		return nil
	}
	severity := api.Severity(s)
	return &severity
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
