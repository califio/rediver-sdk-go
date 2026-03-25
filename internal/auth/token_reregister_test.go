package auth

import (
	"context"
	"fmt"
	"testing"
)

func TestReregister_CallsGenerateFn(t *testing.T) {
	tm := NewTokenManager("cluster-tok")
	tm.agentToken.Store("old-token")
	tm.genReq = GenerateTokenRequest{
		ClusterToken: "cluster-tok",
		Scanner:      "subdomain",
		Persistent:   false,
	}

	called := false
	tm.SetGenerateTokenFunc(func(ctx context.Context, req GenerateTokenRequest) (*GenerateTokenResponse, error) {
		called = true
		return &GenerateTokenResponse{
			Token:   "new-token",
			AgentID: "agent-123",
		}, nil
	})

	err := tm.Reregister(context.Background(), "old-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("generateFn was not called")
	}
	if tm.AgentToken() != "new-token" {
		t.Fatalf("token not updated: got %s", tm.AgentToken())
	}
	if tm.AgentID() != "agent-123" {
		t.Fatalf("agentID not updated: got %s", tm.AgentID())
	}
}

func TestReregister_WorkerMode_CallsGenerateFn(t *testing.T) {
	// Worker mode scannerAgents use their own refreshToken(), but the agent-level
	// Reregister (used by pullJob) always calls generateFn regardless of runMode.
	tm := NewTokenManager("cluster-tok")
	tm.agentToken.Store("old-token")
	tm.genReq = GenerateTokenRequest{Scanner: "scanner-a"}

	called := false
	tm.SetGenerateTokenFunc(func(ctx context.Context, req GenerateTokenRequest) (*GenerateTokenResponse, error) {
		called = true
		return &GenerateTokenResponse{Token: "new-worker-token", AgentID: "agent-w"}, nil
	})

	err := tm.Reregister(context.Background(), "old-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("generateFn was not called")
	}
	if tm.AgentToken() != "new-worker-token" {
		t.Fatalf("token not updated: got %s", tm.AgentToken())
	}
}

func TestReregister_CIMode_CallsGenerateFn(t *testing.T) {
	tm := NewTokenManager("cluster-tok")
	tm.agentToken.Store("old-token")
	tm.genReq = GenerateTokenRequest{Scanner: "scanner-ci"}

	called := false
	tm.SetGenerateTokenFunc(func(ctx context.Context, req GenerateTokenRequest) (*GenerateTokenResponse, error) {
		called = true
		return &GenerateTokenResponse{Token: "new-ci-token"}, nil
	})

	err := tm.Reregister(context.Background(), "old-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("generateFn was not called for CI mode")
	}
}

func TestReregister_SkipsIfTokenAlreadyChanged(t *testing.T) {
	tm := NewTokenManager("cluster-tok")
	tm.agentToken.Store("already-refreshed-token")

	called := false
	tm.SetGenerateTokenFunc(func(ctx context.Context, req GenerateTokenRequest) (*GenerateTokenResponse, error) {
		called = true
		return &GenerateTokenResponse{Token: "should-not-reach"}, nil
	})

	// Pass stale oldToken — should detect token already changed and skip
	err := tm.Reregister(context.Background(), "old-stale-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Fatal("generateFn should not be called when token already changed")
	}
	if tm.AgentToken() != "already-refreshed-token" {
		t.Fatal("token should remain unchanged")
	}
}

func TestReregister_GenerateFnError(t *testing.T) {
	tm := NewTokenManager("cluster-tok")
	tm.agentToken.Store("old-token")

	tm.SetGenerateTokenFunc(func(ctx context.Context, req GenerateTokenRequest) (*GenerateTokenResponse, error) {
		return nil, fmt.Errorf("server unreachable")
	})

	err := tm.Reregister(context.Background(), "old-token")
	if err == nil {
		t.Fatal("expected error when generateFn fails")
	}
	if tm.AgentToken() != "old-token" {
		t.Fatal("token should remain unchanged on error")
	}
}

func TestReregister_NilGenerateFn(t *testing.T) {
	tm := NewTokenManager("cluster-tok")
	tm.agentToken.Store("old-token")
	// generateFn not set

	err := tm.Reregister(context.Background(), "old-token")
	if err == nil {
		t.Fatal("expected error when generateFn is nil")
	}
}
