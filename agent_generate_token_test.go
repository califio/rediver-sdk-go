package rediver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"

	scannerv1 "github.com/califio/rediver-sdk-go/internal/gen/grpc/scanner/v1"
	"github.com/califio/rediver-sdk-go/internal/gen/grpc/scanner/v1/scannerv1connect"
)

// --- minimal Connect service implementations for test ---

// testScannerService captures RegisterAgent requests during Agent init.
type testScannerService struct {
	scannerv1connect.UnimplementedScannerServiceHandler
	runnerID string
	lastReq  *scannerv1.RegisterAgentRequest
}

func (s *testScannerService) RegisterAgent(_ context.Context, req *connect.Request[scannerv1.RegisterAgentRequest]) (*connect.Response[scannerv1.RegisterAgentResponse], error) {
	s.lastReq = req.Msg
	return connect.NewResponse(&scannerv1.RegisterAgentResponse{RunnerId: s.runnerID}), nil
}

// newTestConnectServer mounts only the services used by Agent init:
// ScannerService (for RegisterAgent). Returns the server URL and a reference
// to testScannerService so tests can inspect captured requests.
func newTestConnectServer(t *testing.T, svc *testScannerService) string {
	t.Helper()
	mux := http.NewServeMux()
	path, handler := scannerv1connect.NewScannerServiceHandler(svc)
	mux.Handle(path, handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestNewAgent_TaskDirectJobRegistersScanner(t *testing.T) {
	t.Parallel()

	svc := &testScannerService{runnerID: "runner-1"}
	serverURL := newTestConnectServer(t, svc)

	scanner := NewScanner("calif-audit", []TargetType{TargetTypeRepository}, nil)
	a, err := NewAgent("agent-token", scanner, WithServerURL(serverURL))
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}

	// initSession with directJobID — Task mode: ephemeral=false, no sync, bound to job.
	if err := a.initSession(context.Background(), false, false, "job-123"); err != nil {
		t.Fatalf("initSession() error = %v", err)
	}

	req := svc.lastReq
	if req == nil {
		t.Fatal("RegisterAgent was never called")
	}
	if len(req.GetScanners()) != 1 || req.GetScanners()[0] != "calif-audit" {
		t.Fatalf("scanners = %v, want [calif-audit]", req.GetScanners())
	}
}

func TestNewAgent_TaskPollRegistersWithoutRunnerID(t *testing.T) {
	t.Parallel()

	svc := &testScannerService{runnerID: "runner-1"}
	serverURL := newTestConnectServer(t, svc)

	scanner := NewScanner("calif-audit", []TargetType{TargetTypeRepository}, nil)
	a, err := NewAgent("agent-token", scanner, WithServerURL(serverURL))
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}

	// initSession without directJobID — normal Task mode poll: ephemeral, no job binding.
	if err := a.initSession(context.Background(), false, false, ""); err != nil {
		t.Fatalf("initSession() error = %v", err)
	}

	req := svc.lastReq
	if req == nil {
		t.Fatal("RegisterAgent was never called")
	}
	if req.RunnerId != nil {
		t.Fatalf("runner_id = %v, want nil", *req.RunnerId)
	}
}
