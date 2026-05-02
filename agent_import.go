package rediver

import (
	"context"

	agentv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/agent/v1"
)

const agentMaxBatchSize = 500

func (a *agent) importResult(ctx context.Context, jobID string, res Result, headSHA string) {
	if domains := res.GetDomains(); len(domains) > 0 {
		a.importDomains(ctx, jobID, domains)
	}
	if services := res.GetServices(); len(services) > 0 {
		a.importServices(ctx, jobID, services)
	}
	if findings := res.GetWebFindings(); len(findings) > 0 {
		a.importWebFindings(ctx, jobID, findings)
	}
	if findings := res.GetSASTFindings(); len(findings) > 0 {
		if headSHA != "" {
			for i := range findings {
				if findings[i].CommitSha == "" {
					findings[i].CommitSha = headSHA
				}
			}
		}
		a.importSASTFindings(ctx, jobID, findings)
	}
}

func (a *agent) importDomains(ctx context.Context, jobID string, domains []Domain) {
	apiDomains := toProtoDomains(domains)
	for i := 0; i < len(apiDomains); i += agentMaxBatchSize {
		end := min(i+agentMaxBatchSize, len(apiDomains))
		chunk := apiDomains[i:end]
		err := a.retrier.Do(ctx, func() error {
			return a.client.PushAssets(ctx, &agentv1.PushAssetsRequest{
				JobId:   jobID,
				Domains: chunk,
			})
		})
		if err != nil {
			a.logger.Error("import domains failed", "job_id", jobID, "error", err)
		}
	}
}

func (a *agent) importServices(ctx context.Context, jobID string, services []Service) {
	apiServices := toProtoServices(services)
	for i := 0; i < len(apiServices); i += agentMaxBatchSize {
		end := min(i+agentMaxBatchSize, len(apiServices))
		chunk := apiServices[i:end]
		err := a.retrier.Do(ctx, func() error {
			return a.client.PushAssets(ctx, &agentv1.PushAssetsRequest{
				JobId:    jobID,
				Services: chunk,
			})
		})
		if err != nil {
			a.logger.Error("import services failed", "job_id", jobID, "error", err)
		}
	}
}

func (a *agent) importWebFindings(ctx context.Context, jobID string, findings []WebFinding) {
	err := a.retrier.Do(ctx, func() error {
		return a.client.PushFindings(ctx, &agentv1.PushFindingsRequest{
			JobId:       jobID,
			WebFindings: toProtoWebFindings(findings),
		})
	})
	if err != nil {
		a.logger.Error("import web findings failed", "job_id", jobID, "error", err)
	}
}

func (a *agent) importSASTFindings(ctx context.Context, jobID string, findings []SASTFinding) {
	err := a.retrier.Do(ctx, func() error {
		return a.client.PushFindings(ctx, &agentv1.PushFindingsRequest{
			JobId:        jobID,
			CodeFindings: toProtoSASTFindings(findings),
		})
	})
	if err != nil {
		a.logger.Error("import SAST findings failed", "job_id", jobID, "error", err)
	}
}
