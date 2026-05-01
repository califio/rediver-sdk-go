package rediver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	agentv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/agent/v1"
	"buf.build/gen/go/rediver/api/connectrpc/go/agent/v1/agentv1connect"
	"connectrpc.com/connect"
)

// --- test service implementations ---

// dispatchTokenService captures the last GenerateToken request.
type dispatchTokenService struct {
	agentv1connect.UnimplementedTokenServiceHandler

	mu      sync.Mutex
	lastReq *agentv1.GenerateTokenRequest
}

func (s *dispatchTokenService) GenerateToken(_ context.Context, req *connect.Request[agentv1.GenerateTokenRequest]) (*connect.Response[agentv1.GenerateTokenResponse], error) {
	s.mu.Lock()
	s.lastReq = req.Msg
	s.mu.Unlock()
	return connect.NewResponse(&agentv1.GenerateTokenResponse{
		AgentId: "agent-1",
		Token:   "agent-token-1",
	}), nil
}

// dispatchAgentService captures UpdateScanner calls.
type dispatchAgentService struct {
	agentv1connect.UnimplementedAgentServiceHandler

	mu          sync.Mutex
	updateReqs  []*agentv1.UpdateScannerRequest
	updateSeen  chan struct{}
}

func (s *dispatchAgentService) Heartbeat(_ context.Context, _ *connect.Request[agentv1.HeartbeatRequest]) (*connect.Response[agentv1.HeartbeatResponse], error) {
	return connect.NewResponse(&agentv1.HeartbeatResponse{}), nil
}

func (s *dispatchAgentService) UpdateScanner(ctx context.Context, req *connect.Request[agentv1.UpdateScannerRequest]) (*connect.Response[agentv1.UpdateScannerResponse], error) {
	// Verify X-Token header was injected by authRetryTransport.
	if got := req.Header().Get("X-Token"); got != "agent-token-1" {
		// We can't call t.Errorf from here (no testing.T); store for assertion in test.
	}
	s.mu.Lock()
	s.updateReqs = append(s.updateReqs, req.Msg)
	s.mu.Unlock()
	select {
	case s.updateSeen <- struct{}{}:
	default:
	}
	return connect.NewResponse(&agentv1.UpdateScannerResponse{}), nil
}

// dispatchJobService returns no-job (empty response) for PollJob.
type dispatchJobService struct {
	agentv1connect.UnimplementedJobServiceHandler
}

func (s *dispatchJobService) PollJob(_ context.Context, _ *connect.Request[agentv1.PollJobRequest]) (*connect.Response[agentv1.PollJobResponse], error) {
	return connect.NewResponse(&agentv1.PollJobResponse{}), nil
}

// newDispatchSmokeServer creates an httptest server with all required Connect services.
func newDispatchSmokeServer(t *testing.T, tokenSvc *dispatchTokenService, agentSvc *dispatchAgentService, jobSvc *dispatchJobService) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(agentv1connect.NewTokenServiceHandler(tokenSvc))
	mux.Handle(agentv1connect.NewAgentServiceHandler(agentSvc))
	mux.Handle(agentv1connect.NewJobServiceHandler(jobSvc))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestDispatch_SmokeSyncsScannerMetadata(t *testing.T) {
	t.Parallel()

	tokenSvc := &dispatchTokenService{}
	agentSvc := &dispatchAgentService{updateSeen: make(chan struct{}, 1)}
	jobSvc := &dispatchJobService{}

	serverURL := newDispatchSmokeServer(t, tokenSvc, agentSvc, jobSvc)

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

	runner, err := NewRunner(serverURL, "cluster-token",
		WithDispatcherMetadataSync(),
		WithPollInterval(time.Second),
	)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	if err := runner.Add(NewScanner(
		"calif-audit",
		[]TargetType{TargetTypeRepository, TargetTypeService},
		nil,
		WithDisplayName("Calif Audit"),
		WithRawParamsSchema(schema),
	)); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- runner.Dispatch(ctx, func(context.Context, PulledJob) error { return nil })
	}()

	select {
	case <-agentSvc.updateSeen:
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

	// --- assertions on GenerateToken ---
	tokenSvc.mu.Lock()
	gotTokenReq := tokenSvc.lastReq
	tokenSvc.mu.Unlock()

	if gotTokenReq == nil {
		t.Fatal("GenerateToken was never called")
	}
	if gotTokenReq.GetScanner() != "calif-audit" {
		t.Fatalf("token scanner = %q, want calif-audit", gotTokenReq.GetScanner())
	}
	if !gotTokenReq.GetPersistent() {
		t.Fatal("expected dispatcher token request to be persistent")
	}

	// --- assertions on UpdateScanner ---
	agentSvc.mu.Lock()
	updateReqs := agentSvc.updateReqs
	agentSvc.mu.Unlock()

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
	if assetTypes[0] != agentv1.AssetType_ASSET_TYPE_REPOSITORY {
		t.Fatalf("asset_types[0] = %v, want ASSET_TYPE_REPOSITORY", assetTypes[0])
	}
	if assetTypes[1] != agentv1.AssetType_ASSET_TYPE_SERVICE {
		t.Fatalf("asset_types[1] = %v, want ASSET_TYPE_SERVICE", assetTypes[1])
	}
}
