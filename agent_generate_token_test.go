package rediver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	agentv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/agent/v1"
	"buf.build/gen/go/rediver/api/connectrpc/go/agent/v1/agentv1connect"
	"connectrpc.com/connect"
)

// --- minimal Connect service implementations for test ---

// testTokenService is a minimal TokenServiceHandler that captures the last request.
type testTokenService struct {
	agentv1connect.UnimplementedTokenServiceHandler
	agentID string
	token   string
	lastReq *agentv1.GenerateTokenRequest
}

func (s *testTokenService) GenerateToken(_ context.Context, req *connect.Request[agentv1.GenerateTokenRequest]) (*connect.Response[agentv1.GenerateTokenResponse], error) {
	s.lastReq = req.Msg
	return connect.NewResponse(&agentv1.GenerateTokenResponse{
		AgentId: s.agentID,
		Token:   s.token,
	}), nil
}

// newTestConnectServer mounts only the services used by Agent init:
// TokenService (for GenerateToken). Returns the server URL and a reference to
// testTokenService so tests can inspect captured requests.
func newTestConnectServer(t *testing.T, svc *testTokenService) string {
	t.Helper()
	mux := http.NewServeMux()
	path, handler := agentv1connect.NewTokenServiceHandler(svc)
	mux.Handle(path, handler)
	// AgentService Heartbeat / UpdateScanner — not called during init; mount
	// an unimplemented handler so the mux doesn't 404.
	mux.Handle(agentv1connect.NewAgentServiceHandler(&agentv1connect.UnimplementedAgentServiceHandler{}))
	mux.Handle(agentv1connect.NewJobServiceHandler(&agentv1connect.UnimplementedJobServiceHandler{}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestNewAgent_TaskDirectJobIncludesJobIdInGenerateToken(t *testing.T) {
	t.Parallel()

	svc := &testTokenService{agentID: "agent-1", token: "agent-token-1"}
	serverURL := newTestConnectServer(t, svc)

	scanner := NewScanner("calif-audit", []TargetType{TargetTypeRepository}, nil)
	a, err := NewAgent("cluster-token", scanner, WithServerURL(serverURL))
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}

	// initSession with directJobID — Task mode: ephemeral=false, no sync, bound to job.
	if err := a.initSession(context.Background(), false, false, "job-123"); err != nil {
		t.Fatalf("initSession() error = %v", err)
	}

	req := svc.lastReq
	if req == nil {
		t.Fatal("GenerateToken was never called")
	}
	if req.GetScanner() != "calif-audit" {
		t.Fatalf("scanner = %q, want calif-audit", req.GetScanner())
	}
	if req.GetPersistent() {
		t.Fatal("expected task token request to be ephemeral (persistent=false)")
	}
	if req.JobId == nil || *req.JobId != "job-123" {
		t.Fatalf("job_id = %v, want job-123", req.JobId)
	}
}

func TestNewAgent_TaskPollDoesNotIncludeJobIdInGenerateToken(t *testing.T) {
	t.Parallel()

	svc := &testTokenService{agentID: "agent-1", token: "agent-token-1"}
	serverURL := newTestConnectServer(t, svc)

	scanner := NewScanner("calif-audit", []TargetType{TargetTypeRepository}, nil)
	a, err := NewAgent("cluster-token", scanner, WithServerURL(serverURL))
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}

	// initSession without directJobID — normal Task mode poll: ephemeral, no job binding.
	if err := a.initSession(context.Background(), false, false, ""); err != nil {
		t.Fatalf("initSession() error = %v", err)
	}

	req := svc.lastReq
	if req == nil {
		t.Fatal("GenerateToken was never called")
	}
	if req.GetScanner() != "calif-audit" {
		t.Fatalf("scanner = %q, want calif-audit", req.GetScanner())
	}
	if req.GetPersistent() {
		t.Fatal("expected task token request to be ephemeral (persistent=false)")
	}
	if req.JobId != nil {
		t.Fatalf("job_id = %v, want nil", *req.JobId)
	}
}
