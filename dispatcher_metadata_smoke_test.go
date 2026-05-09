package rediver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	"buf.build/gen/go/rediver/api/connectrpc/go/scanner/v1/scannerv1connect"
	scannerv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/scanner/v1"
)

// --- test service implementations ---

// dispatchScannerService implements ScannerService, capturing RegisterMachine and UpdateScanner calls.
type dispatchScannerService struct {
	scannerv1connect.UnimplementedScannerServiceHandler

	mu         sync.Mutex
	lastRegReq *scannerv1.RegisterMachineRequest
	updateReqs []*scannerv1.UpdateScannerRequest
	updateSeen chan struct{}
}

func (s *dispatchScannerService) RegisterMachine(_ context.Context, req *connect.Request[scannerv1.RegisterMachineRequest]) (*connect.Response[scannerv1.RegisterMachineResponse], error) {
	s.mu.Lock()
	s.lastRegReq = req.Msg
	s.mu.Unlock()
	return connect.NewResponse(&scannerv1.RegisterMachineResponse{RunnerId: "runner-1"}), nil
}

func (s *dispatchScannerService) Heartbeat(_ context.Context, _ *connect.Request[scannerv1.HeartbeatRequest]) (*connect.Response[scannerv1.HeartbeatResponse], error) {
	return connect.NewResponse(&scannerv1.HeartbeatResponse{}), nil
}

func (s *dispatchScannerService) UpdateScanner(_ context.Context, req *connect.Request[scannerv1.UpdateScannerRequest]) (*connect.Response[scannerv1.UpdateScannerResponse], error) {
	s.mu.Lock()
	s.updateReqs = append(s.updateReqs, req.Msg)
	s.mu.Unlock()
	select {
	case s.updateSeen <- struct{}{}:
	default:
	}
	return connect.NewResponse(&scannerv1.UpdateScannerResponse{}), nil
}

func (s *dispatchScannerService) PollJob(_ context.Context, _ *connect.Request[scannerv1.PollJobRequest]) (*connect.Response[scannerv1.PollJobResponse], error) {
	return connect.NewResponse(&scannerv1.PollJobResponse{}), nil
}

// newDispatchSmokeServer creates an httptest server with all required Connect services.
func newDispatchSmokeServer(t *testing.T, scannerSvc *dispatchScannerService) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(scannerv1connect.NewScannerServiceHandler(scannerSvc))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestDispatch_SmokeSyncsScannerMetadata(t *testing.T) {
	t.Parallel()

	scannerSvc := &dispatchScannerService{updateSeen: make(chan struct{}, 1)}

	serverURL := newDispatchSmokeServer(t, scannerSvc)

	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"audit_mode": map[string]interface{}{
				"type":  "string",
				"title": "Audit Mode",
			},
			"model": map[string]interface{}{
				"type": "string",
			},
		},
		"additionalProperties": false,
	}

	agent, err := NewAgent("agent-token",
		NewScanner(
			"calif-audit",
			[]TargetType{TargetTypeRepository, TargetTypeService},
			nil,
			WithDisplayName("Calif Audit"),
			WithRawParamsSchema(schema),
		),
		WithServerURL(serverURL),
		WithDispatcherMetadataSync(),
		WithPollInterval(time.Second),
	)
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- agent.Dispatch(ctx, func(context.Context, PulledJob) error { return nil })
	}()

	select {
	case <-scannerSvc.updateSeen:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for scanner metadata sync")
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Dispatch() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for dispatcher shutdown")
	}

	// --- assertions on RegisterMachine ---
	scannerSvc.mu.Lock()
	gotRegisterReq := scannerSvc.lastRegReq
	scannerSvc.mu.Unlock()

	if gotRegisterReq == nil {
		t.Fatal("RegisterMachine was never called")
	}

	// --- assertions on UpdateScanner ---
	scannerSvc.mu.Lock()
	updateReqs := scannerSvc.updateReqs
	scannerSvc.mu.Unlock()

	if len(updateReqs) != 1 {
		t.Fatalf("updateCalls = %d, want 1", len(updateReqs))
	}
	updateReq := updateReqs[0]

	if updateReq.GetName() != "calif-audit" {
		t.Fatalf("update name = %q, want calif-audit", updateReq.GetName())
	}
	if updateReq.GetDisplayName() != "Calif Audit" {
		t.Fatalf("display_name = %q, want Calif Audit", updateReq.GetDisplayName())
	}
	if updateReq.GetParamsSchema() == nil {
		t.Fatal("expected params_schema to be sent")
	}
	propsVal, ok := updateReq.GetParamsSchema().GetFields()["properties"]
	if !ok {
		t.Fatal("expected params_schema.properties field")
	}
	propsStruct := propsVal.GetStructValue()
	if propsStruct == nil {
		t.Fatal("params_schema.properties is not a Struct")
	}
	if _, ok := propsStruct.GetFields()["audit_mode"]; !ok {
		t.Fatal("expected audit_mode in params_schema.properties")
	}
	assetTypes := updateReq.GetAssetTypes()
	if len(assetTypes) != 2 {
		t.Fatalf("asset_types len = %d, want 2", len(assetTypes))
	}
	if assetTypes[0] != scannerv1.AssetType_ASSET_TYPE_REPOSITORY {
		t.Fatalf("asset_types[0] = %v, want ASSET_TYPE_REPOSITORY", assetTypes[0])
	}
	if assetTypes[1] != scannerv1.AssetType_ASSET_TYPE_SERVICE {
		t.Fatalf("asset_types[1] = %v, want ASSET_TYPE_SERVICE", assetTypes[1])
	}
}
