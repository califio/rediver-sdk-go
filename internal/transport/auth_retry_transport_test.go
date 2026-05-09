package transport

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/califio/rediver-sdk-go/internal/auth"
)

// mockRoundTripper returns configurable responses per call.
type mockRoundTripper struct {
	calls   atomic.Int32
	handler func(req *http.Request, callNum int) (*http.Response, error)
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	n := int(m.calls.Add(1))
	return m.handler(req, n)
}

func makeResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{},
	}
}

// TestAuthTransport_AgentPlane_UsesXToken verifies that requests without a job
// token in context get X-Token injected (agent-plane calls).
func TestAuthTransport_AgentPlane_UsesXToken(t *testing.T) {
	tm := auth.NewTokenManager("agent-token")

	var capturedToken string
	mock := &mockRoundTripper{
		handler: func(req *http.Request, _ int) (*http.Response, error) {
			capturedToken = req.Header.Get("X-Token")
			return makeResponse(200, `{"ok":true}`), nil
		},
	}

	tr := &authTransport{base: mock, tm: tm}
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "http://localhost/api/heartbeat", nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if capturedToken != "agent-token" {
		t.Fatalf("X-Token = %q, want agent-token", capturedToken)
	}
}

// TestAuthTransport_JobPlane_UsesBearer verifies that a request whose context
// carries a job token gets Authorization: Bearer injected (job-plane calls).
func TestAuthTransport_JobPlane_UsesBearer(t *testing.T) {
	tm := auth.NewTokenManager("agent-token")

	var capturedAuth, capturedXToken string
	mock := &mockRoundTripper{
		handler: func(req *http.Request, _ int) (*http.Response, error) {
			capturedAuth = req.Header.Get("Authorization")
			capturedXToken = req.Header.Get("X-Token")
			return makeResponse(200, `{"ok":true}`), nil
		},
	}

	tr := &authTransport{base: mock, tm: tm}
	ctx := WithJobToken(context.Background(), "job-jwt")
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://localhost/api/job/start", nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if capturedAuth != "Bearer job-jwt" {
		t.Fatalf("Authorization = %q, want Bearer job-jwt", capturedAuth)
	}
	if capturedXToken != "" {
		t.Fatalf("X-Token = %q, want empty (job-plane should not set X-Token)", capturedXToken)
	}
}

// TestAuthTransport_NoToken_NoHeaders verifies that when no agent token is set
// and no job token is in context, neither header is added.
func TestAuthTransport_NoToken_NoHeaders(t *testing.T) {
	tm := auth.NewTokenManager("") // empty agent token

	var capturedAuth, capturedXToken string
	mock := &mockRoundTripper{
		handler: func(req *http.Request, _ int) (*http.Response, error) {
			capturedAuth = req.Header.Get("Authorization")
			capturedXToken = req.Header.Get("X-Token")
			return makeResponse(200, "ok"), nil
		},
	}

	tr := &authTransport{base: mock, tm: tm}
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "http://localhost/api/test", nil)
	if _, err := tr.RoundTrip(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedAuth != "" {
		t.Fatalf("Authorization = %q, want empty", capturedAuth)
	}
	if capturedXToken != "" {
		t.Fatalf("X-Token = %q, want empty", capturedXToken)
	}
}

// TestAuthTransport_401_SurfacesToCaller verifies that 401 responses are
// returned directly to the caller — no retry or refresh in the new model.
func TestAuthTransport_401_SurfacesToCaller(t *testing.T) {
	tm := auth.NewTokenManager("agent-token")

	calls := 0
	mock := &mockRoundTripper{
		handler: func(req *http.Request, _ int) (*http.Response, error) {
			calls++
			return makeResponse(401, "unauthorized"), nil
		},
	}

	tr := &authTransport{base: mock, tm: tm}
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "http://localhost/api/test", nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	if calls != 1 {
		t.Fatalf("expected exactly 1 call (no retry), got %d", calls)
	}
}
