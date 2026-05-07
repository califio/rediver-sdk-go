package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"

	"github.com/califio/rediver-sdk-go/internal/auth"
	authv1 "github.com/califio/rediver-sdk-go/internal/gen/grpc/auth/v1"
	"github.com/califio/rediver-sdk-go/internal/gen/grpc/auth/v1/authv1connect"
	scannerv1 "github.com/califio/rediver-sdk-go/internal/gen/grpc/scanner/v1"
	"github.com/califio/rediver-sdk-go/internal/gen/grpc/scanner/v1/scannerv1connect"
)

type authFlowTokenService struct {
	authv1connect.UnimplementedTokenServiceHandler

	t              *testing.T
	createJobCalls int
}

func (s *authFlowTokenService) CreateJobToken(_ context.Context, req *connect.Request[authv1.CreateJobTokenRequest]) (*connect.Response[authv1.CreateJobTokenResponse], error) {
	s.t.Helper()
	s.createJobCalls++
	if got := req.Header().Get("X-Token"); got != "agent-token" {
		s.t.Fatalf("CreateJobToken X-Token = %q, want agent-token", got)
	}
	if got := req.Header().Get("Authorization"); got != "" {
		s.t.Fatalf("CreateJobToken Authorization = %q, want empty", got)
	}
	if got := req.Msg.GetJobId(); got != "job-1" {
		s.t.Fatalf("CreateJobToken job_id = %q, want job-1", got)
	}
	if got := req.Msg.GetRunnerId(); got != "runner-1" {
		s.t.Fatalf("CreateJobToken runner_id = %q, want runner-1", got)
	}
	return connect.NewResponse(&authv1.CreateJobTokenResponse{Token: "job-jwt"}), nil
}

type authFlowScannerService struct {
	scannerv1connect.UnimplementedScannerServiceHandler

	t                  *testing.T
	registerSeen       bool
	pollSeen           bool
	jobStartBearerSeen bool
}

func (s *authFlowScannerService) RegisterAgent(_ context.Context, req *connect.Request[scannerv1.RegisterAgentRequest]) (*connect.Response[scannerv1.RegisterAgentResponse], error) {
	s.t.Helper()
	s.registerSeen = true
	if got := req.Header().Get("X-Token"); got != "agent-token" {
		s.t.Fatalf("RegisterAgent X-Token = %q, want agent-token", got)
	}
	if got := req.Header().Get("Authorization"); got != "" {
		s.t.Fatalf("RegisterAgent Authorization = %q, want empty", got)
	}
	return connect.NewResponse(&scannerv1.RegisterAgentResponse{RunnerId: "runner-1"}), nil
}

func (s *authFlowScannerService) PollJob(_ context.Context, req *connect.Request[scannerv1.PollJobRequest]) (*connect.Response[scannerv1.PollJobResponse], error) {
	s.t.Helper()
	s.pollSeen = true
	if got := req.Header().Get("X-Token"); got != "agent-token" {
		s.t.Fatalf("PollJob X-Token = %q, want agent-token", got)
	}
	if got := req.Header().Get("Authorization"); got != "" {
		s.t.Fatalf("PollJob Authorization = %q, want empty", got)
	}
	return connect.NewResponse(&scannerv1.PollJobResponse{
		JobId:   strPtr("job-1"),
		Scanner: strPtr("scanner-1"),
	}), nil
}

func (s *authFlowScannerService) JobStart(_ context.Context, req *connect.Request[scannerv1.JobStartRequest]) (*connect.Response[scannerv1.JobStartResponse], error) {
	s.t.Helper()
	if got := req.Header().Get("Authorization"); got != "Bearer job-jwt" {
		s.t.Fatalf("JobStart Authorization = %q, want Bearer job-jwt", got)
	}
	if got := req.Header().Get("X-Token"); got != "" {
		s.t.Fatalf("JobStart X-Token = %q, want empty", got)
	}
	s.jobStartBearerSeen = true
	return connect.NewResponse(&scannerv1.JobStartResponse{Success: true}), nil
}

func TestClient_UsesAgentTokenForAgentPlaneAndBearerForJobPlane(t *testing.T) {
	t.Parallel()

	tokenSvc := &authFlowTokenService{t: t}
	scannerSvc := &authFlowScannerService{t: t}

	mux := http.NewServeMux()
	mux.Handle(authv1connect.NewTokenServiceHandler(tokenSvc))
	mux.Handle(scannerv1connect.NewScannerServiceHandler(scannerSvc))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	tm := auth.NewTokenManager("agent-token")
	tm.SetToken("agent-token")
	tm.SetAgentID("runner-1")

	client, err := NewClient(server.URL, tm, server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	runnerID, err := client.RegisterAgent(context.Background(), &scannerv1.RegisterAgentRequest{
		Scanners: []string{"scanner-1"},
	})
	if err != nil {
		t.Fatalf("RegisterAgent() error = %v", err)
	}
	if runnerID != "runner-1" {
		t.Fatalf("runnerID = %q, want runner-1", runnerID)
	}

	jobID, scanner, err := client.DoPollJob(context.Background(), 0)
	if err != nil {
		t.Fatalf("DoPollJob() error = %v", err)
	}
	if jobID != "job-1" || scanner != "scanner-1" {
		t.Fatalf("poll = (%q, %q), want (job-1, scanner-1)", jobID, scanner)
	}

	if err := client.JobStart(context.Background(), "job-1"); err != nil {
		t.Fatalf("JobStart() error = %v", err)
	}

	if !scannerSvc.registerSeen {
		t.Fatal("RegisterAgent was not called")
	}
	if !scannerSvc.pollSeen {
		t.Fatal("PollJob was not called")
	}
	if !scannerSvc.jobStartBearerSeen {
		t.Fatal("JobStart did not receive bearer auth")
	}
	if tokenSvc.createJobCalls != 1 {
		t.Fatalf("CreateJobToken calls = %d, want 1", tokenSvc.createJobCalls)
	}
}
