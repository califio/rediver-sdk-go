package rediver

import (
	"context"

	agentv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/agent/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

// syncScannerMetadata pushes scanner-declared metadata (display name, params schema,
// asset types, retest support) to the backend on agent startup. Errors are logged,
// not fatal — metadata sync is best-effort.
func (a *Agent) syncScannerMetadata(ctx context.Context) {
	name := a.scanner.Name()
	req := &agentv1.UpdateScannerRequest{Name: name}
	var needsUpdate bool

	if dn, ok := a.scanner.(interface{ DisplayName() string }); ok {
		if displayName := dn.DisplayName(); displayName != "" {
			req.DisplayName = &displayName
			needsUpdate = true
		}
	}

	if ps, ok := a.scanner.(interface{ ParamsSchema() map[string]interface{} }); ok {
		if schema := ps.ParamsSchema(); schema != nil {
			s, err := structpb.NewStruct(schema)
			if err == nil {
				req.ParamsSchema = s
				needsUpdate = true
			}
		}
	} else if ps := a.scanner.Params(); len(ps) > 0 {
		schema := ParamsToJSONSchema(ps)
		if schema != nil {
			s, err := structpb.NewStruct(schema)
			if err == nil {
				req.ParamsSchema = s
				needsUpdate = true
			}
		}
	}

	if types := a.scanner.AssetTypes(); len(types) > 0 {
		assetTypes := make([]agentv1.AssetType, len(types))
		for i, t := range types {
			assetTypes[i] = toProtoAssetType(t)
		}
		req.AssetTypes = assetTypes
		needsUpdate = true
	}

	if rc, ok := a.scanner.(interface{ SupportsRetest() bool }); ok {
		if supportsRetest := rc.SupportsRetest(); supportsRetest {
			req.SupportsRetest = &supportsRetest
			needsUpdate = true
		}
	}

	if !needsUpdate {
		return
	}
	if err := a.client.UpdateScanner(ctx, req); err != nil {
		a.logger.Warn("failed to sync scanner metadata", "error", err)
	} else {
		a.logger.Info("scanner metadata synced")
	}
}
