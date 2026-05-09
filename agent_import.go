package rediver

import (
	"context"

	scannerv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/scanner/v1"
)

const agentMaxBatchSize = 500

func (a *Agent) importResult(ctx context.Context, jobID string, res Result, headSHA string) {
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

func (a *Agent) importDomains(ctx context.Context, jobID string, domains []Domain) {
	apiDomains := toProtoDomains(domains)
	for i := 0; i < len(apiDomains); i += agentMaxBatchSize {
		end := min(i+agentMaxBatchSize, len(apiDomains))
		chunk := apiDomains[i:end]
		err := a.retrier.Do(ctx, func() error {
			// ctx carries job token (WithJobToken); job_id resolved from JWT claim server-side.
			return a.client.PushAssets(ctx, &scannerv1.PushAssetsRequest{
				Domains: chunk,
			})
		})
		if err != nil {
			a.logger.Error("import domains failed", "job_id", jobID, "error", err)
		}
	}
}

func (a *Agent) importServices(ctx context.Context, jobID string, services []Service) {
	apiServices := toProtoServices(services)
	for i := 0; i < len(apiServices); i += agentMaxBatchSize {
		end := min(i+agentMaxBatchSize, len(apiServices))
		chunk := apiServices[i:end]
		err := a.retrier.Do(ctx, func() error {
			// ctx carries job token; job_id resolved from JWT claim server-side.
			return a.client.PushAssets(ctx, &scannerv1.PushAssetsRequest{
				Services: chunk,
			})
		})
		if err != nil {
			a.logger.Error("import services failed", "job_id", jobID, "error", err)
		}
	}
}

func (a *Agent) importWebFindings(ctx context.Context, jobID string, findings []WebFinding) {
	err := a.retrier.Do(ctx, func() error {
		// ctx carries job token; job_id resolved from JWT claim server-side.
		return a.client.PushFindings(ctx, &scannerv1.PushFindingsRequest{
			WebFindings: toProtoWebFindings(findings),
		})
	})
	if err != nil {
		a.logger.Error("import web findings failed", "job_id", jobID, "error", err)
	}
}

func (a *Agent) importSASTFindings(ctx context.Context, jobID string, findings []SASTFinding) {
	err := a.retrier.Do(ctx, func() error {
		// ctx carries job token; job_id resolved from JWT claim server-side.
		return a.client.PushFindings(ctx, &scannerv1.PushFindingsRequest{
			CodeFindings: toProtoSASTFindings(findings),
		})
	})
	if err != nil {
		a.logger.Error("import SAST findings failed", "job_id", jobID, "error", err)
	}
}
