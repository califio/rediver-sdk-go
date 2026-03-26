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
	calls    atomic.Int32
	handler  func(req *http.Request, callNum int) (*http.Response, error)
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

func TestAuthRetryTransport_401_RefreshesAndRetries(t *testing.T) {
	tm := auth.NewTokenManager("cluster-token")
	tm.SetToken("expired-token")

	// Wire generate-token to return a new token
	var generateCalled atomic.Int32
	tm.SetGenerateTokenFunc(func(ctx context.Context, req auth.GenerateTokenRequest) (*auth.GenerateTokenResponse, error) {
		generateCalled.Add(1)
		return &auth.GenerateTokenResponse{
			AgentId: strPtr("agent-123"),
			Token:   strPtr("fresh-token"),
		}, nil
	})

	var capturedTokens []string
	mock := &mockRoundTripper{
		handler: func(req *http.Request, callNum int) (*http.Response, error) {
			capturedTokens = append(capturedTokens, req.Header.Get("X-Token"))
			if callNum == 1 {
				return makeResponse(401, "unauthorized"), nil
			}
			return makeResponse(200, `{"ok":true}`), nil
		},
	}

	transport := &authRetryTransport{
		base:         mock,
		tokenManager: tm,
	}

	req, _ := http.NewRequestWithContext(context.Background(), "GET", "http://localhost/api/agent/heartbeat", nil)
	resp, err := transport.RoundTrip(req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if mock.calls.Load() != 2 {
		t.Fatalf("expected 2 calls to base transport, got %d", mock.calls.Load())
	}
	if generateCalled.Load() != 1 {
		t.Fatalf("expected 1 generate-token call, got %d", generateCalled.Load())
	}

	// First call used expired token, second used fresh token
	if capturedTokens[0] != "expired-token" {
		t.Fatalf("first call should use expired-token, got %q", capturedTokens[0])
	}
	if capturedTokens[1] != "fresh-token" {
		t.Fatalf("second call should use fresh-token, got %q", capturedTokens[1])
	}
}

func TestAuthRetryTransport_401_RefreshFails_Returns401(t *testing.T) {
	tm := auth.NewTokenManager("cluster-token")
	tm.SetToken("expired-token")

	// Wire generate-token to fail (simulate cluster token revoked)
	tm.SetGenerateTokenFunc(func(ctx context.Context, req auth.GenerateTokenRequest) (*auth.GenerateTokenResponse, error) {
		return nil, &http.MaxBytesError{} // any error
	})

	mock := &mockRoundTripper{
		handler: func(req *http.Request, callNum int) (*http.Response, error) {
			return makeResponse(401, "unauthorized"), nil
		},
	}

	transport := &authRetryTransport{
		base:         mock,
		tokenManager: tm,
	}

	req, _ := http.NewRequestWithContext(context.Background(), "GET", "http://localhost/api/test", nil)
	resp, err := transport.RoundTrip(req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401 (refresh failed), got %d", resp.StatusCode)
	}
	if mock.calls.Load() != 1 {
		t.Fatalf("expected 1 call (no retry after failed refresh), got %d", mock.calls.Load())
	}
}

func TestAuthRetryTransport_200_NoRefresh(t *testing.T) {
	tm := auth.NewTokenManager("cluster-token")
	tm.SetToken("valid-token")

	var generateCalled atomic.Int32
	tm.SetGenerateTokenFunc(func(ctx context.Context, req auth.GenerateTokenRequest) (*auth.GenerateTokenResponse, error) {
		generateCalled.Add(1)
		return nil, nil
	})

	mock := &mockRoundTripper{
		handler: func(req *http.Request, callNum int) (*http.Response, error) {
			return makeResponse(200, `{"ok":true}`), nil
		},
	}

	transport := &authRetryTransport{
		base:         mock,
		tokenManager: tm,
	}

	req, _ := http.NewRequestWithContext(context.Background(), "GET", "http://localhost/api/test", nil)
	resp, err := transport.RoundTrip(req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if mock.calls.Load() != 1 {
		t.Fatalf("expected 1 call (no retry), got %d", mock.calls.Load())
	}
	if generateCalled.Load() != 0 {
		t.Fatalf("expected 0 generate-token calls, got %d", generateCalled.Load())
	}
}

func TestAuthRetryTransport_401_RetryWithBody(t *testing.T) {
	tm := auth.NewTokenManager("cluster-token")
	tm.SetToken("expired-token")

	tm.SetGenerateTokenFunc(func(ctx context.Context, req auth.GenerateTokenRequest) (*auth.GenerateTokenResponse, error) {
		return &auth.GenerateTokenResponse{Token: strPtr("fresh-token"), AgentId: strPtr("agent-1")}, nil
	})

	var capturedBodies []string
	mock := &mockRoundTripper{
		handler: func(req *http.Request, callNum int) (*http.Response, error) {
			body, _ := io.ReadAll(req.Body)
			capturedBodies = append(capturedBodies, string(body))
			if callNum == 1 {
				return makeResponse(401, "unauthorized"), nil
			}
			return makeResponse(200, "ok"), nil
		},
	}

	transport := &authRetryTransport{
		base:         mock,
		tokenManager: tm,
	}

	req, _ := http.NewRequestWithContext(context.Background(), "POST", "http://localhost/api/test",
		strings.NewReader(`{"scanner":"subdomain"}`))
	resp, err := transport.RoundTrip(req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Both calls should have the same body
	if len(capturedBodies) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(capturedBodies))
	}
	if capturedBodies[0] != `{"scanner":"subdomain"}` {
		t.Fatalf("first call body mismatch: %q", capturedBodies[0])
	}
	if capturedBodies[1] != `{"scanner":"subdomain"}` {
		t.Fatalf("retry call body mismatch (body should be re-readable): %q", capturedBodies[1])
	}
}
