package rediver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// newTestServer creates a mock server that tracks token validity, pull counts, and register counts.
// invalidateToken returns a func to simulate server-side token expiry.
func newTestServer(t *testing.T, jobID string) (serverURL string, invalidateToken func(), pullCount *atomic.Int32, regCount *atomic.Int32, cleanup func()) {
	t.Helper()

	var validToken atomic.Value
	validToken.Store("")
	pCount := &atomic.Int32{}
	rCount := &atomic.Int32{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/agent/register":
			rCount.Add(1)
			newTok := fmt.Sprintf("token-v%d", rCount.Load())
			validToken.Store(newTok)
			json.NewEncoder(w).Encode(map[string]any{
				"agent_id":   "agent-1",
				"token":      newTok,
				"expires_at": "2099-01-01T00:00:00Z",
				"scanners": []map[string]any{
					{"name": "subdomain", "request_name": "subdomain", "display_name": "Subdomain"},
				},
			})

		case "/api/agent/job/request":
			pCount.Add(1)
			reqToken := r.Header.Get("X-Token")
			if reqToken != validToken.Load().(string) {
				w.WriteHeader(401)
				json.NewEncoder(w).Encode(map[string]string{"error": "token invalid"})
				return
			}
			if jobID == "" {
				w.WriteHeader(204)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"job_id": jobID})

		default:
			w.WriteHeader(404)
		}
	}))

	invalidate := func() {
		validToken.Store("__invalidated__")
	}

	return server.URL, invalidate, pCount, rCount, server.Close
}

// registerAgent creates an agent, registers a scanner, and performs initial registration.
func registerAgent(t *testing.T, serverURL string) *Agent {
	t.Helper()

	agent, err := NewAgent(serverURL, "test-cluster-token", WithWorkerMode())
	if err != nil {
		t.Fatal(err)
	}
	agent.Register(NewScanner("subdomain", []TargetType{TargetTypeDomain}, nil))

	// Perform real registration (sets token, caches regReq for re-registration)
	if err := agent.registerAndInitPool(context.Background()); err != nil {
		t.Fatal(err)
	}

	return agent
}

// TestPullJob_401_TriggersReregisterAndRetry emulates the production scenario:
// 1. Agent registers successfully (gets token-v1)
// 2. Token expires server-side (simulates ExpiredAgentTokenCleanupJob)
// 3. pullJob gets 401, auto-re-registers (gets token-v2), retries pull
// 4. Second pull succeeds
func TestPullJob_401_TriggersReregisterAndRetry(t *testing.T) {
	serverURL, invalidate, pullCount, regCount, cleanup := newTestServer(t, "job-abc-123")
	defer cleanup()

	agent := registerAgent(t, serverURL)

	// Initial registration happened
	if regCount.Load() != 1 {
		t.Fatalf("expected 1 initial registration, got %d", regCount.Load())
	}

	// Simulate token expiry (backend cleanup job)
	invalidate()

	// pullJob: 401 → re-register → retry → success
	jobID, err := agent.pullJob(context.Background())
	if err != nil {
		t.Fatalf("pullJob should succeed after re-registration, got: %v", err)
	}
	if jobID != "job-abc-123" {
		t.Fatalf("expected job-abc-123, got %s", jobID)
	}

	// 1 initial + 1 re-registration = 2 total
	if regCount.Load() != 2 {
		t.Fatalf("expected 2 total registrations (1 initial + 1 re-register), got %d", regCount.Load())
	}
	// 1 failed (401) + 1 retry = 2 pulls
	if pullCount.Load() != 2 {
		t.Fatalf("expected 2 pull attempts, got %d", pullCount.Load())
	}
}

// TestPullJob_401_ReregisterFails_ReturnsError verifies that pullJob returns
// an error when both pull and re-registration fail.
func TestPullJob_401_ReregisterFails_ReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/agent/register":
			// Registration also fails
			w.WriteHeader(503)
			w.Write([]byte("service unavailable"))
		case "/api/agent/job/request":
			w.WriteHeader(401)
			json.NewEncoder(w).Encode(map[string]string{"error": "token invalid"})
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	agent, err := NewAgent(server.URL, "test-cluster-token", WithWorkerMode())
	if err != nil {
		t.Fatal(err)
	}
	agent.Register(NewScanner("subdomain", []TargetType{TargetTypeDomain}, nil))

	_, err = agent.pullJob(context.Background())
	if err == nil {
		t.Fatal("expected error when re-registration fails")
	}
}

// TestPullJob_200_NoReregister verifies that successful pulls don't trigger re-registration.
func TestPullJob_200_NoReregister(t *testing.T) {
	serverURL, _, pullCount, regCount, cleanup := newTestServer(t, "job-ok")
	defer cleanup()

	agent := registerAgent(t, serverURL)

	jobID, err := agent.pullJob(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if jobID != "job-ok" {
		t.Fatalf("expected job-ok, got %s", jobID)
	}
	// Only initial registration, no re-registration
	if regCount.Load() != 1 {
		t.Fatalf("expected 1 registration (initial only), got %d", regCount.Load())
	}
	if pullCount.Load() != 1 {
		t.Fatalf("expected 1 pull attempt, got %d", pullCount.Load())
	}
}

// TestPullJob_204_NoJob_NoReregister verifies 204 returns ErrNoJobAvailable
// without triggering re-registration.
func TestPullJob_204_NoJob_NoReregister(t *testing.T) {
	serverURL, _, _, regCount, cleanup := newTestServer(t, "")
	defer cleanup()

	agent := registerAgent(t, serverURL)

	_, err := agent.pullJob(context.Background())
	if !errors.Is(err, ErrNoJobAvailable) {
		t.Fatalf("expected ErrNoJobAvailable, got %v", err)
	}
	if regCount.Load() != 1 {
		t.Fatalf("expected 1 registration (initial only), got %d", regCount.Load())
	}
}
