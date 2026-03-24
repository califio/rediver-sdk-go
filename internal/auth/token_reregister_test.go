package auth

import (
	"context"
	"fmt"
	"testing"
)

func TestReregister_WorkerMode_CallsRegisterFn(t *testing.T) {
	tm := NewTokenManager("cluster-tok", RunModeWorker, nil)
	tm.agentToken.Store("old-token")
	tm.regReq = RegistrationRequest{Scanners: []string{"subdomain"}}

	registerCalled := false
	tm.SetRegisterFunc(func(ctx context.Context, req RegistrationRequest) (*RegistrationResponse, error) {
		registerCalled = true
		return &RegistrationResponse{
			Token:   "new-worker-token",
			AgentID: "agent-123",
		}, nil
	})

	err := tm.Reregister(context.Background(), "old-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !registerCalled {
		t.Fatal("registerFn was not called")
	}
	if tm.AgentToken() != "new-worker-token" {
		t.Fatalf("token not updated: got %s", tm.AgentToken())
	}
	if tm.AgentID() != "agent-123" {
		t.Fatalf("agentID not updated: got %s", tm.AgentID())
	}
}

func TestReregister_TaskMode_CallsConnectFn(t *testing.T) {
	tm := NewTokenManager("cluster-tok", RunModeTask, nil)
	tm.agentToken.Store("old-token")

	connectCalled := false
	tm.SetConnectFunc(func(ctx context.Context, req ConnectRequest) (*ConnectResponse, error) {
		connectCalled = true
		return &ConnectResponse{
			Token:   "new-task-token",
			AgentID: "cluster-id",
		}, nil
	})

	err := tm.Reregister(context.Background(), "old-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !connectCalled {
		t.Fatal("connectFn was not called")
	}
	if tm.AgentToken() != "new-task-token" {
		t.Fatalf("token not updated: got %s", tm.AgentToken())
	}
}

func TestReregister_CIMode_CallsConnectFn(t *testing.T) {
	tm := NewTokenManager("cluster-tok", RunModeCI, nil)
	tm.agentToken.Store("old-token")

	connectCalled := false
	tm.SetConnectFunc(func(ctx context.Context, req ConnectRequest) (*ConnectResponse, error) {
		connectCalled = true
		return &ConnectResponse{Token: "new-ci-token"}, nil
	})

	err := tm.Reregister(context.Background(), "old-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !connectCalled {
		t.Fatal("connectFn was not called for CI mode")
	}
}

func TestReregister_SkipsIfTokenAlreadyChanged(t *testing.T) {
	tm := NewTokenManager("cluster-tok", RunModeWorker, nil)
	tm.agentToken.Store("already-refreshed-token")

	registerCalled := false
	tm.SetRegisterFunc(func(ctx context.Context, req RegistrationRequest) (*RegistrationResponse, error) {
		registerCalled = true
		return &RegistrationResponse{Token: "should-not-reach"}, nil
	})

	// Pass stale oldToken — should detect token already changed and skip
	err := tm.Reregister(context.Background(), "old-stale-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if registerCalled {
		t.Fatal("registerFn should not be called when token already changed")
	}
	if tm.AgentToken() != "already-refreshed-token" {
		t.Fatal("token should remain unchanged")
	}
}

func TestReregister_WorkerMode_RegisterFnError(t *testing.T) {
	tm := NewTokenManager("cluster-tok", RunModeWorker, nil)
	tm.agentToken.Store("old-token")

	tm.SetRegisterFunc(func(ctx context.Context, req RegistrationRequest) (*RegistrationResponse, error) {
		return nil, fmt.Errorf("server unreachable")
	})

	err := tm.Reregister(context.Background(), "old-token")
	if err == nil {
		t.Fatal("expected error when registerFn fails")
	}
	if tm.AgentToken() != "old-token" {
		t.Fatal("token should remain unchanged on error")
	}
}

func TestReregister_TaskMode_ConnectFnError(t *testing.T) {
	tm := NewTokenManager("cluster-tok", RunModeTask, nil)
	tm.agentToken.Store("old-token")

	tm.SetConnectFunc(func(ctx context.Context, req ConnectRequest) (*ConnectResponse, error) {
		return nil, fmt.Errorf("server unreachable")
	})

	err := tm.Reregister(context.Background(), "old-token")
	if err == nil {
		t.Fatal("expected error when connectFn fails")
	}
	if tm.AgentToken() != "old-token" {
		t.Fatal("token should remain unchanged on error")
	}
}

func TestReregister_WorkerMode_NilRegisterFn(t *testing.T) {
	tm := NewTokenManager("cluster-tok", RunModeWorker, nil)
	tm.agentToken.Store("old-token")
	// registerFn not set

	err := tm.Reregister(context.Background(), "old-token")
	if err == nil {
		t.Fatal("expected error when registerFn is nil")
	}
}

func TestReregister_TaskMode_NilConnectFn(t *testing.T) {
	tm := NewTokenManager("cluster-tok", RunModeTask, nil)
	tm.agentToken.Store("old-token")
	// connectFn not set

	err := tm.Reregister(context.Background(), "old-token")
	if err == nil {
		t.Fatal("expected error when connectFn is nil")
	}
}
