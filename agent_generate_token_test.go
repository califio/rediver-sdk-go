package rediver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"

	"buf.build/gen/go/rediver/api/connectrpc/go/scanner/v1/scannerv1connect"
	scannerv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/scanner/v1"
)

// --- minimal Connect service implementations for test ---

// testScannerService captures RegisterMachine requests during Agent init.
type testScannerService struct {
	scannerv1connect.UnimplementedScannerServiceHandler
	runnerID string
	lastReq  *scannerv1.RegisterMachineRequest
}

func (s *testScannerService) RegisterMachine(_ context.Context, req *connect.Request[scannerv1.RegisterMachineRequest]) (*connect.Response[scannerv1.RegisterMachineResponse], error) {
	s.lastReq = req.Msg
	return connect.NewResponse(&scannerv1.RegisterMachineResponse{RunnerId: s.runnerID}), nil
}

// newTestConnectServer mounts only the services used by Agent init:
// ScannerService (for RegisterMachine). Returns the server URL and a reference
// to testScannerService so tests can inspect captured requests.
func newTestConnectServer(t *testing.T, svc *testScannerService) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(scannerv1connect.NewScannerServiceHandler(svc))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestInitSession_RegistersMachine(t *testing.T) {
	t.Parallel()

	svc := &testScannerService{runnerID: "runner-1"}
	serverURL := newTestConnectServer(t, svc)

	scanner := NewScanner("calif-audit", []TargetType{TargetTypeRepository}, nil)
	a, err := NewAgent("agent-token", scanner, WithServerURL(serverURL))
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}

	if err := a.initSession(context.Background(), false, false); err != nil {
		t.Fatalf("initSession() error = %v", err)
	}

	req := svc.lastReq
	if req == nil {
		t.Fatal("RegisterMachine was never called")
	}
	// runner_id is nil on first registration (no prior machine ID).
	if req.RunnerId != nil {
		t.Fatalf("runner_id = %v, want nil on first registration", *req.RunnerId)
	}
	// runnerID should be propagated back to agent after registration.
	if a.runnerID != "runner-1" {
		t.Fatalf("a.runnerID = %q, want runner-1", a.runnerID)
	}
}

func TestInitSession_SetsTokenManagerRunnerID(t *testing.T) {
	t.Parallel()

	svc := &testScannerService{runnerID: "runner-42"}
	serverURL := newTestConnectServer(t, svc)

	scanner := NewScanner("calif-audit", []TargetType{TargetTypeRepository}, nil)
	a, err := NewAgent("agent-token", scanner, WithServerURL(serverURL))
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}

	if err := a.initSession(context.Background(), true, false); err != nil {
		t.Fatalf("initSession() error = %v", err)
	}

	if got := a.tokenManager.RunnerID(); got != "runner-42" {
		t.Fatalf("tokenManager.RunnerID() = %q, want runner-42", got)
	}
}
