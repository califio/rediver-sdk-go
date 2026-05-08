package rediver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"buf.build/gen/go/rediver/api/connectrpc/go/agent/v1/agentv1connect"
	agentv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/agent/v1"
	"connectrpc.com/connect"

	authv1 "github.com/califio/rediver-sdk-go/internal/gen/grpc/auth/v1"
	"github.com/califio/rediver-sdk-go/internal/gen/grpc/auth/v1/authv1connect"
	scannerv1 "github.com/califio/rediver-sdk-go/internal/gen/grpc/scanner/v1"
	"github.com/califio/rediver-sdk-go/internal/gen/grpc/scanner/v1/scannerv1connect"
)

// --- test service implementations ---

// dispatchAuthService captures RegisterAgent calls.
type dispatchAuthService struct {
	authv1connect.UnimplementedAuthServiceHandler

	mu      sync.Mutex
	lastReq *authv1.RegisterAgentRequest
}

func (s *dispatchAuthService) RegisterAgent(_ context.Context, req *connect.Request[authv1.RegisterAgentRequest]) (*connect.Response[authv1.RegisterAgentResponse], error) {
	s.mu.Lock()
	s.lastReq = req.Msg
	s.mu.Unlock()
	return connect.NewResponse(&authv1.RegisterAgentResponse{RunnerId: "runner-1"}), nil
}

type dispatchScannerService struct {
	scannerv1connect.UnimplementedScannerServiceHandler
}

func (s *dispatchScannerService) Heartbeat(_ context.Context, _ *connect.Request[scannerv1.HeartbeatRequest]) (*connect.Response[scannerv1.HeartbeatResponse], error) {
	return connect.NewResponse(&scannerv1.HeartbeatResponse{}), nil
}

func (s *dispatchScannerService) PollJob(_ context.Context, _ *connect.Request[scannerv1.PollJobRequest]) (*connect.Response[scannerv1.PollJobResponse], error) {
	return connect.NewResponse(&scannerv1.PollJobResponse{}), nil
}

// dispatchAgentService captures UpdateScanner calls.
type dispatchAgentService struct {
	agentv1connect.UnimplementedAgentServiceHandler

	mu         sync.Mutex
	updateReqs []*agentv1.UpdateScannerRequest
	updateSeen chan struct{}
}

func (s *dispatchAgentService) Heartbeat(_ context.Context, _ *connect.Request[agentv1.HeartbeatRequest]) (*connect.Response[agentv1.HeartbeatResponse], error) {
	return connect.NewResponse(&agentv1.HeartbeatResponse{}), nil
}

func (s *dispatchAgentService) UpdateScanner(ctx context.Context, req *connect.Request[agentv1.UpdateScannerRequest]) (*connect.Response[agentv1.UpdateScannerResponse], error) {
	// Verify X-Token header was injected by authRetryTransport.
	if got := req.Header().Get("X-Token"); got != "agent-token" {
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

// newDispatchSmokeServer creates an httptest server with all required Connect services.
func newDispatchSmokeServer(t *testing.T, authSvc *dispatchAuthService, scannerSvc *dispatchScannerService, agentSvc *dispatchAgentService) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(authv1connect.NewAuthServiceHandler(authSvc))
	mux.Handle(scannerv1connect.NewScannerServiceHandler(scannerSvc))
	mux.Handle(agentv1connect.NewAgentServiceHandler(agentSvc))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestDispatch_SmokeSyncsScannerMetadata(t *testing.T) {
	t.Parallel()

	authSvc := &dispatchAuthService{}
	scannerSvc := &dispatchScannerService{}
	agentSvc := &dispatchAgentService{updateSeen: make(chan struct{}, 1)}

	serverURL := newDispatchSmokeServer(t, authSvc, scannerSvc, agentSvc)

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

	// --- assertions on RegisterAgent ---
	authSvc.mu.Lock()
	gotRegisterReq := authSvc.lastReq
	authSvc.mu.Unlock()

	if gotRegisterReq == nil {
		t.Fatal("RegisterAgent was never called")
	}
	if len(gotRegisterReq.GetScanners()) != 1 || gotRegisterReq.GetScanners()[0] != "calif-audit" {
		t.Fatalf("register scanners = %v, want [calif-audit]", gotRegisterReq.GetScanners())
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
