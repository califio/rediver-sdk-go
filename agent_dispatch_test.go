package rediver

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"

	"buf.build/gen/go/rediver/api/connectrpc/go/scanner/v1/scannerv1connect"
	scannerv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/scanner/v1"
)

// dispatchJobService returns a configurable sequence of jobs from PollJob.
type dispatchJobService struct {
	scannerv1connect.UnimplementedJobServiceHandler

	mu    sync.Mutex
	queue []pollResult
}

type pollResult struct {
	jobID   string
	scanner string
}

func (s *dispatchJobService) PollJob(_ context.Context, _ *connect.Request[scannerv1.PollJobRequest]) (*connect.Response[scannerv1.PollJobResponse], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		return connect.NewResponse(&scannerv1.PollJobResponse{}), nil
	}
	next := s.queue[0]
	s.queue = s.queue[1:]
	return connect.NewResponse(&scannerv1.PollJobResponse{
		JobId:   &next.jobID,
		Scanner: &next.scanner,
	}), nil
}

type dispatchScannerSvc struct {
	scannerv1connect.UnimplementedScannerServiceHandler
}

func (s *dispatchScannerSvc) RegisterMachine(_ context.Context, _ *connect.Request[scannerv1.RegisterMachineRequest]) (*connect.Response[scannerv1.RegisterMachineResponse], error) {
	return connect.NewResponse(&scannerv1.RegisterMachineResponse{RunnerId: "runner-1"}), nil
}

func (s *dispatchScannerSvc) Heartbeat(_ context.Context, _ *connect.Request[scannerv1.HeartbeatRequest]) (*connect.Response[scannerv1.HeartbeatResponse], error) {
	return connect.NewResponse(&scannerv1.HeartbeatResponse{}), nil
}

func newDispatchTestServer(t *testing.T, jobSvc *dispatchJobService) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(scannerv1connect.NewScannerServiceHandler(&dispatchScannerSvc{}))
	mux.Handle(scannerv1connect.NewJobServiceHandler(jobSvc))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestDispatch_HandlerCalledSynchronously(t *testing.T) {
	jobSvc := &dispatchJobService{
		queue: []pollResult{
			{jobID: "job-1", scanner: "subdomain"},
			{jobID: "job-2", scanner: "subdomain"},
			{jobID: "job-3", scanner: "subdomain"},
		},
	}
	serverURL := newDispatchTestServer(t, jobSvc)

	var received []PulledJob
	var mu sync.Mutex
	var handlerGoroutines sync.Map

	agent, err := NewAgent("agent-token",
		NewScanner("subdomain", []TargetType{TargetTypeDomain}, nil),
		WithServerURL(serverURL),
		WithPollInterval(time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- agent.Dispatch(ctx, func(_ context.Context, job PulledJob) error {
			handlerGoroutines.Store(job.JobID, true)
			mu.Lock()
			received = append(received, job)
			mu.Unlock()
			time.Sleep(50 * time.Millisecond)
			return nil
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Dispatch error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 3 {
		t.Fatalf("handler called %d times, want 3", len(received))
	}
	for i, want := range []string{"job-1", "job-2", "job-3"} {
		if received[i].JobID != want {
			t.Errorf("received[%d].JobID = %q, want %q", i, received[i].JobID, want)
		}
		if received[i].Scanner != "subdomain" {
			t.Errorf("received[%d].Scanner = %q, want subdomain", i, received[i].Scanner)
		}
	}
}

func TestDispatch_HandlerErrorDoesNotStopPolling(t *testing.T) {
	jobSvc := &dispatchJobService{
		queue: []pollResult{
			{jobID: "fail-1", scanner: "s"},
			{jobID: "ok-2", scanner: "s"},
		},
	}
	serverURL := newDispatchTestServer(t, jobSvc)

	var count atomic.Int32
	agent, err := NewAgent("agent-token",
		NewScanner("s", []TargetType{TargetTypeDomain}, nil),
		WithServerURL(serverURL),
		WithPollInterval(time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- agent.Dispatch(ctx, func(_ context.Context, job PulledJob) error {
			count.Add(1)
			if job.JobID == "fail-1" {
				return fmt.Errorf("handler error")
			}
			return nil
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Dispatch error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if got := count.Load(); got != 2 {
		t.Errorf("handler called %d times, want 2", got)
	}
}

func TestDispatch_LongPollingUsesWaitSeconds(t *testing.T) {
	var gotWaitSeconds atomic.Int32
	var pollCount atomic.Int32

	longPollJobSvc := &longPollJobService{
		onPoll: func(ws int32) {
			gotWaitSeconds.Store(ws)
			pollCount.Add(1)
		},
	}

	mux := http.NewServeMux()
	mux.Handle(scannerv1connect.NewScannerServiceHandler(&dispatchScannerSvc{}))
	mux.Handle(scannerv1connect.NewJobServiceHandler(longPollJobSvc))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	agent, err := NewAgent("agent-token",
		NewScanner("s", []TargetType{TargetTypeDomain}, nil),
		WithServerURL(srv.URL),
		WithDispatchMode(DispatchLongPolling),
		WithLongPollWait(30*time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_ = agent.Dispatch(ctx, func(_ context.Context, _ PulledJob) error { return nil })

	if got := gotWaitSeconds.Load(); got != 30 {
		t.Errorf("waitSeconds = %d, want 30", got)
	}
	if pollCount.Load() == 0 {
		t.Error("PollJob was never called")
	}
}

// longPollJobService captures WaitSeconds from PollJob requests.
type longPollJobService struct {
	scannerv1connect.UnimplementedJobServiceHandler
	onPoll func(waitSeconds int32)
}

func (s *longPollJobService) PollJob(_ context.Context, req *connect.Request[scannerv1.PollJobRequest]) (*connect.Response[scannerv1.PollJobResponse], error) {
	if s.onPoll != nil {
		s.onPoll(req.Msg.GetWaitSeconds())
	}
	return connect.NewResponse(&scannerv1.PollJobResponse{}), nil
}
