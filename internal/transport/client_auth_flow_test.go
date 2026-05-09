package transport

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"connectrpc.com/connect"

	"buf.build/gen/go/rediver/api/connectrpc/go/scanner/v1/scannerv1connect"
	scannerv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/scanner/v1"
	"github.com/califio/rediver-sdk-go/internal/auth"
)

// authFlowScannerService implements ScannerService/JobService for auth-flow tests.
// Validates that:
//   - Agent-plane RPCs (RegisterMachine, CreateJobToken, PollJob) carry X-Token.
//   - Job-plane RPCs (JobStart) carry Authorization: Bearer and no X-Token.
type authFlowScannerService struct {
	scannerv1connect.UnimplementedScannerServiceHandler
	scannerv1connect.UnimplementedJobServiceHandler

	t                  *testing.T
	registerSeen       bool
	createJobCalls     int
	pollSeen           bool
	jobStartBearerSeen bool
}

func (s *authFlowScannerService) RegisterMachine(_ context.Context, req *connect.Request[scannerv1.RegisterMachineRequest]) (*connect.Response[scannerv1.RegisterMachineResponse], error) {
	s.t.Helper()
	s.registerSeen = true
	if got := req.Header().Get("X-Token"); got != "agent-token" {
		s.t.Fatalf("RegisterMachine X-Token = %q, want agent-token", got)
	}
	if got := req.Header().Get("Authorization"); got != "" {
		s.t.Fatalf("RegisterMachine Authorization = %q, want empty", got)
	}
	return connect.NewResponse(&scannerv1.RegisterMachineResponse{RunnerId: "runner-1"}), nil
}

func (s *authFlowScannerService) CreateJobToken(_ context.Context, req *connect.Request[scannerv1.CreateJobTokenRequest]) (*connect.Response[scannerv1.CreateJobTokenResponse], error) {
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
	return connect.NewResponse(&scannerv1.CreateJobTokenResponse{Token: "job-jwt"}), nil
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

	svc := &authFlowScannerService{t: t}

	mux := http.NewServeMux()
	mux.Handle(scannerv1connect.NewScannerServiceHandler(svc))
	mux.Handle(scannerv1connect.NewJobServiceHandler(svc))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	tm := auth.NewTokenManager("agent-token")
	tm.SetRunnerID("runner-1")

	client, err := NewClient(server.URL, tm, server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	// Agent-plane: RegisterMachine uses X-Token.
	runnerID, err := client.RegisterAgent(context.Background(), &scannerv1.RegisterMachineRequest{})
	if err != nil {
		t.Fatalf("RegisterAgent() error = %v", err)
	}
	if runnerID != "runner-1" {
		t.Fatalf("runnerID = %q, want runner-1", runnerID)
	}

	// Agent-plane: PollJob uses X-Token.
	jobID, scanner, err := client.DoPollJob(context.Background(), 0)
	if err != nil {
		t.Fatalf("DoPollJob() error = %v", err)
	}
	if jobID != "job-1" || scanner != "scanner-1" {
		t.Fatalf("poll = (%q, %q), want (job-1, scanner-1)", jobID, scanner)
	}

	// Agent-plane: CreateJobToken uses X-Token, forwards runnerID.
	jobToken, err := client.CreateJobToken(context.Background(), "job-1", "runner-1")
	if err != nil {
		t.Fatalf("CreateJobToken() error = %v", err)
	}

	// Job-plane: JobStart uses Bearer from context-keyed job token.
	jobCtx := WithJobToken(context.Background(), jobToken)
	if err := client.JobStart(jobCtx); err != nil {
		t.Fatalf("JobStart() error = %v", err)
	}

	if !svc.registerSeen {
		t.Fatal("RegisterMachine was not called")
	}
	if !svc.pollSeen {
		t.Fatal("PollJob was not called")
	}
	if !svc.jobStartBearerSeen {
		t.Fatal("JobStart did not receive bearer auth")
	}
	if svc.createJobCalls != 1 {
		t.Fatalf("CreateJobToken calls = %d, want 1", svc.createJobCalls)
	}
}

// TestConcurrentJobs_DistinctTokensNoLeakage verifies the per-context job-token
// contract: N goroutines each tag ctx with their own token via WithJobToken and
// make a fake RPC. Every request must carry exactly that goroutine's token in
// Authorization — no cross-goroutine token leakage is possible because
// context.WithValue is immutable.
func TestConcurrentJobs_DistinctTokensNoLeakage(t *testing.T) {
	t.Parallel()

	const goroutines = 50

	// capturedTokens[i] will be set by goroutine i when its request fires.
	capturedTokens := make([]string, goroutines)
	var mu sync.Mutex

	// Fake RoundTripper that records the Authorization header from each request.
	fake := &fakeRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			auth := req.Header.Get("Authorization")
			// Extract token index from the Authorization header value
			// (format: "Bearer token-<i>").
			mu.Lock()
			for i := range capturedTokens {
				if auth == fmt.Sprintf("Bearer token-%d", i) {
					capturedTokens[i] = auth
					break
				}
			}
			mu.Unlock()
			return &http.Response{
				StatusCode: 200,
				Body:       http.NoBody,
				Header:     http.Header{},
			}, nil
		},
	}

	tm := auth.NewTokenManager("agent-token")
	tr := &authTransport{base: fake, tm: tm}

	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			jobToken := fmt.Sprintf("token-%d", idx)
			ctx := WithJobToken(context.Background(), jobToken)
			req, _ := http.NewRequestWithContext(ctx, "POST", "http://localhost/job/start", nil)
			resp, err := tr.RoundTrip(req)
			if err != nil {
				t.Errorf("goroutine %d: unexpected error: %v", idx, err)
				return
			}
			resp.Body.Close()
		}(i)
	}
	wg.Wait()

	// Every slot must have been set to exactly the right token.
	for i := range goroutines {
		want := fmt.Sprintf("Bearer token-%d", i)
		if capturedTokens[i] != want {
			t.Errorf("goroutine %d: Authorization = %q, want %q", i, capturedTokens[i], want)
		}
	}
}

// fakeRoundTripper invokes a handler function for every request.
type fakeRoundTripper struct {
	handler func(*http.Request) (*http.Response, error)
}

func (f *fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f.handler(req)
}
