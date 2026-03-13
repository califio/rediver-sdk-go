package rediver

import (
	"context"
	"fmt"
	"testing"
)

func TestListenForJobs_NilHandler(t *testing.T) {
	agent, err := NewAgent("http://localhost", "test-token")
	if err != nil {
		t.Fatal(err)
	}
	agent.Register(NewScanner("test", []TargetType{TargetTypeDomain}, nil))

	err = agent.ListenForJobs(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil handler")
	}
}

func TestListenForJobs_NoScanners(t *testing.T) {
	agent, err := NewAgent("http://localhost", "test-token")
	if err != nil {
		t.Fatal(err)
	}

	err = agent.ListenForJobs(context.Background(), func(ctx context.Context, jobID string) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error for no scanners")
	}
}

func TestListenForJobs_AlreadyRunning(t *testing.T) {
	agent, err := NewAgent("http://localhost", "test-token")
	if err != nil {
		t.Fatal(err)
	}
	agent.Register(NewScanner("test", []TargetType{TargetTypeDomain}, nil))

	// Simulate already running
	agent.running.Store(true)
	defer agent.running.Store(false)

	err = agent.ListenForJobs(context.Background(), func(ctx context.Context, jobID string) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error for already running")
	}
}

func TestDispatchJob_ExecuteCallsHandler(t *testing.T) {
	called := false
	var receivedID string

	agent, _ := NewAgent("http://localhost", "test-token")
	ctx := context.Background()

	job := &dispatchJob{
		agent: agent,
		handler: func(ctx context.Context, jobID string) error {
			called = true
			receivedID = jobID
			return nil
		},
		ctx:   ctx,
		jobID: "job-123",
	}

	err := job.Execute(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("handler was not called")
	}
	if receivedID != "job-123" {
		t.Fatalf("expected job-123, got %s", receivedID)
	}
}

func TestDispatchJob_ExecuteIgnoresPoolContext(t *testing.T) {
	// Verify that dispatchJob uses its own ctx, not the one passed to Execute
	drainCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	poolCtx, poolCancel := context.WithCancel(context.Background())
	poolCancel() // Cancel pool context immediately

	var handlerCtx context.Context
	agent, _ := NewAgent("http://localhost", "test-token")

	job := &dispatchJob{
		agent: agent,
		handler: func(ctx context.Context, jobID string) error {
			handlerCtx = ctx
			return nil
		},
		ctx:   drainCtx,
		jobID: "job-456",
	}

	_ = job.Execute(poolCtx) // Pass cancelled pool context

	if handlerCtx != drainCtx {
		t.Fatal("handler should receive drainCtx, not pool's ctx")
	}
	if handlerCtx.Err() != nil {
		t.Fatal("handler context should not be cancelled")
	}
}

func TestDispatchJob_ErrorSkipsJobFailedOnCancellation(t *testing.T) {
	// When ctx is cancelled (shutdown), handler errors should NOT trigger JobFailed
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Simulate shutdown

	agent, _ := NewAgent("http://localhost", "test-token")

	job := &dispatchJob{
		agent: agent,
		handler: func(ctx context.Context, jobID string) error {
			return fmt.Errorf("dispatch failed")
		},
		ctx:   ctx,
		jobID: "job-789",
	}

	err := job.Execute(context.Background())
	if err == nil {
		t.Fatal("expected error from handler")
	}
	// ctx.Err() != nil prevents reportJobFailed from being called.
	// The HTTP client exists but would fail on network call — the ctx check
	// correctly short-circuits before that happens.
}
