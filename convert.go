package rediver

import (
	"time"

	agentv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/agent/v1"
)

// toProtoDomains converts SDK Domain slice to proto DnsRecordInput slice.
func toProtoDomains(domains []Domain) []*agentv1.DnsRecordInput {
	result := make([]*agentv1.DnsRecordInput, len(domains))
	for i, d := range domains {
		// Merge A (IPv4) and AAAA (IPv6) into single ip field
		var ips []string
		ips = append(ips, d.A...)
		ips = append(ips, d.AAAA...)

		rec := &agentv1.DnsRecordInput{
			Domain: d.Domain,
			Ip:     ips,
			Mx:     d.MX,
			Ns:     d.NS,
			Txt:    d.TXT,
			Soa:    d.SOA,
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

// toProtoServices converts SDK Service slice to proto ServiceInput slice.
func toProtoServices(services []Service) []*agentv1.ServiceInput {
	result := make([]*agentv1.ServiceInput, len(services))
	for i, s := range services {
		svc := &agentv1.ServiceInput{
			Host: s.Host,
			Port: int32(s.Port),
			Cpes: s.CPEs,
		}
		if s.ServiceName != "" {
			svc.ServiceName = &s.ServiceName
		}
		result[i] = svc
	}
	return result
}

// toProtoWebFindings converts SDK WebFinding slice to proto WebFindingInput slice.
func toProtoWebFindings(findings []WebFinding) []*agentv1.WebFindingInput {
	result := make([]*agentv1.WebFindingInput, len(findings))
	for i, f := range findings {
		wf := &agentv1.WebFindingInput{
			Name:     f.Name,
			Severity: toProtoSeverity(f.Severity),
			Endpoint: f.Endpoint,
			Cwes:     f.CWEs,
			References: f.References,
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

// toProtoSASTFindings converts SDK SASTFinding slice to proto CodeFindingInput slice.
func toProtoSASTFindings(findings []SASTFinding) []*agentv1.CodeFindingInput {
	result := make([]*agentv1.CodeFindingInput, len(findings))
	for i, f := range findings {
		cf := &agentv1.CodeFindingInput{
			Name:      f.Name,
			Severity:  toProtoSeverity(f.Severity),
			File:      f.File,
			Cwes:      f.CWEs,
			References: f.References,
			CodeFlows: toProtoCodeFlows(f.CodeFlows),
			CodeLines: toProtoCodeLines(f.CodeLines),
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

// toProtoCodeFlows converts SDK CodeFlowNode slice to proto CodeFlowNodeInput slice.
func toProtoCodeFlows(nodes []CodeFlowNode) []*agentv1.CodeFlowNodeInput {
	if len(nodes) == 0 {
		return nil
	}
	result := make([]*agentv1.CodeFlowNodeInput, len(nodes))
	for i, n := range nodes {
		node := &agentv1.CodeFlowNodeInput{
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
func toProtoCodeLines(lines []CodeLine) []*agentv1.CodeLine {
	if len(lines) == 0 {
		return nil
	}
	result := make([]*agentv1.CodeLine, len(lines))
	for i, l := range lines {
		result[i] = &agentv1.CodeLine{
			Line:    int32(l.Line),
			Content: l.Content,
		}
	}
	return result
}

// toProtoRawRequests converts SDK HTTPRequest slice to proto RawRequestInput slice.
func toProtoRawRequests(requests []HTTPRequest) []*agentv1.RawRequestInput {
	if len(requests) == 0 {
		return nil
	}
	result := make([]*agentv1.RawRequestInput, len(requests))
	for i, r := range requests {
		ri := &agentv1.RawRequestInput{}
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
