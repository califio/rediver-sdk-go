package rediver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewAgent_TaskDirectJobIncludesJobIdInGenerateToken(t *testing.T) {
	t.Parallel()

	type tokenRequest struct {
		Scanner    string  `json:"scanner"`
		Persistent bool    `json:"persistent"`
		JobId      *string `json:"job_id"`
	}

	var got tokenRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/agent/generate-token" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"agent_id": "agent-1",
			"token":    "agent-token-1",
		})
	}))
	defer server.Close()

	cfg := defaultAgentConfig()
	cfg.runMode = RunModeTask
	cfg.directJobID = "job-123"

	scanner := NewScanner("calif-audit", []TargetType{TargetTypeRepository}, nil)
	agent, err := newAgent(context.Background(), scanner, "cluster-token", server.URL, false, false, cfg)
	if err != nil {
		t.Fatalf("newAgent() error = %v", err)
	}

	if agent == nil {
		t.Fatal("expected agent to be created")
	}
	if got.Scanner != "calif-audit" {
		t.Fatalf("scanner = %q, want calif-audit", got.Scanner)
	}
	if got.Persistent {
		t.Fatal("expected task token request to be ephemeral")
	}
	if got.JobId == nil || *got.JobId != "job-123" {
		t.Fatalf("job_id = %v, want job-123", got.JobId)
	}
}

func TestNewAgent_TaskPollDoesNotIncludeJobIdInGenerateToken(t *testing.T) {
	t.Parallel()

	type tokenRequest struct {
		Scanner    string  `json:"scanner"`
		Persistent bool    `json:"persistent"`
		JobId      *string `json:"job_id"`
	}

	var got tokenRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/agent/generate-token" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"agent_id": "agent-1",
			"token":    "agent-token-1",
		})
	}))
	defer server.Close()

	cfg := defaultAgentConfig()
	cfg.runMode = RunModeTask

	scanner := NewScanner("calif-audit", []TargetType{TargetTypeRepository}, nil)
	agent, err := newAgent(context.Background(), scanner, "cluster-token", server.URL, false, false, cfg)
	if err != nil {
		t.Fatalf("newAgent() error = %v", err)
	}

	if agent == nil {
		t.Fatal("expected agent to be created")
	}
	if got.Scanner != "calif-audit" {
		t.Fatalf("scanner = %q, want calif-audit", got.Scanner)
	}
	if got.Persistent {
		t.Fatal("expected task token request to be ephemeral")
	}
	if got.JobId != nil {
		t.Fatalf("job_id = %v, want nil", *got.JobId)
	}
}
