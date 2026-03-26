// Package transport provides HTTP client utilities with automatic 401 token refresh.
package transport

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/califio/rediver-sdk-go/internal/api"
	"github.com/califio/rediver-sdk-go/internal/auth"
)

// Client wraps the generated API client with TokenManager integration.
// 401 retry is handled transparently at the HTTP transport layer.
type Client struct {
	*api.ClientWithResponses
	tokenManager *auth.TokenManager
	httpClient   *http.Client
	baseURL      string
}

// NewClient creates a transport client with automatic token injection and 401 refresh.
func NewClient(baseURL string, tm *auth.TokenManager, httpClient *http.Client) (*Client, error) {
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	// Wrap the HTTP client's transport with auth retry middleware
	base := httpClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	authClient := &http.Client{
		Timeout: httpClient.Timeout,
		Transport: &authRetryTransport{
			base:         base,
			tokenManager: tm,
		},
	}

	c := &Client{
		tokenManager: tm,
		httpClient:   authClient,
		baseURL:      baseURL,
	}

	// Create API client — token injection handled by authRetryTransport
	apiClient, err := api.NewClientWithResponses(baseURL, api.WithHTTPClient(authClient))
	if err != nil {
		return nil, fmt.Errorf("create api client: %w", err)
	}
	c.ClientWithResponses = apiClient

	// Wire callbacks into TokenManager (breaks circular dependency)
	tm.SetRevokeFunc(c.doRevoke)
	tm.SetGenerateTokenFunc(func(ctx context.Context, req auth.GenerateTokenRequest) (*auth.GenerateTokenResponse, error) {
		return c.DoGenerateToken(ctx, req)
	})

	return c, nil
}

// authRetryTransport injects X-Token header and retries once on 401 after refreshing.
type authRetryTransport struct {
	base         http.RoundTripper
	tokenManager *auth.TokenManager
	mu           sync.Mutex // single-flight token refresh
}

func (t *authRetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Buffer body for potential retry (most SDK requests are small JSON)
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("read request body: %w", err)
		}
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	// Inject current token
	token := t.tokenManager.AgentToken()
	if token != "" {
		req.Header.Set("X-Token", token)
	}

	// First attempt
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	// Not 401 → return as-is
	if resp.StatusCode != 401 {
		return resp, nil
	}

	// 401 → refresh token and retry once
	t.mu.Lock()
	refreshErr := t.tokenManager.GenerateToken(req.Context())
	t.mu.Unlock()
	if refreshErr != nil {
		return resp, nil // return original 401 — caller sees the error
	}

	// Rebuild request with new token and fresh body
	resp.Body.Close()
	if bodyBytes != nil {
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}
	req.Header.Set("X-Token", t.tokenManager.AgentToken())

	return t.base.RoundTrip(req)
}

// --- API methods ---

// DoPollJob calls GET /api/agent/job/poll and returns (jobID, scanner, error).
// Returns ("", "", nil) on 204 No Content (no job available).
func (c *Client) DoPollJob(ctx context.Context) (string, string, error) {
	res, err := c.PollJobWithResponse(ctx)
	if err != nil {
		return "", "", fmt.Errorf("poll-job request: %w", err)
	}
	if res.StatusCode() == 204 {
		return "", "", nil
	}
	if res.StatusCode() >= 400 {
		return "", "", fmt.Errorf("poll-job failed: status %d: %s", res.StatusCode(), string(res.Body))
	}
	if res.JSON200 == nil || res.JSON200.JobId == nil {
		return "", "", nil
	}
	return derefStr(res.JSON200.JobId), derefStr(res.JSON200.Scanner), nil
}

// DoGenerateToken calls POST /api/agent/generate-token for per-scanner token exchange.
// This method is also used by authRetryTransport for 401 refresh.
func (c *Client) DoGenerateToken(ctx context.Context, req auth.GenerateTokenRequest) (*auth.GenerateTokenResponse, error) {
	persistent := req.Persistent
	apiReq := api.GenerateAgentTokenRequest{
		ClusterToken: req.ClusterToken,
		AgentId:      req.AgentId,
		Scanner:      strPtr(req.Scanner),
		Persistent:   &persistent,
		Hostname:     strPtr(req.Hostname),
		IpAddress:    strPtr(req.IPAddress),
		Version:      strPtr(req.Version),
	}
	res, err := c.GenerateAgentTokenWithResponse(ctx, apiReq)
	if err != nil {
		return nil, fmt.Errorf("generate-token request: %w", err)
	}
	if res.StatusCode() >= 400 {
		return nil, fmt.Errorf("generate-token failed: status %d: %s", res.StatusCode(), string(res.Body))
	}
	if res.JSON200 == nil {
		return nil, fmt.Errorf("generate-token failed: empty response")
	}
	return &auth.GenerateTokenResponse{
		AgentID: derefStr(res.JSON200.AgentId),
		Token:   derefStr(res.JSON200.Token),
	}, nil
}

// AgentHeartbeat calls GET /api/agent/heartbeat (expects 204).
func (c *Client) AgentHeartbeat(ctx context.Context) error {
	res, err := c.AgentHeartbeatPingWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("agent heartbeat: %w", err)
	}
	if res.StatusCode() >= 400 {
		return fmt.Errorf("agent heartbeat failed: status %d", res.StatusCode())
	}
	return nil
}

// UpdateScanner calls PATCH /api/agent/scanner.
func (c *Client) UpdateScanner(ctx context.Context, req api.UpdateAgentScannerRequest) error {
	res, err := c.UpdateAgentScannerWithResponse(ctx, req)
	if err != nil {
		return err
	}
	if res.StatusCode() >= 400 {
		return fmt.Errorf("update scanner failed: status %d: %s", res.StatusCode(), string(res.Body))
	}
	return nil
}

// GetArtifactPresignedURL returns a presigned download URL for the given artifact.
func (c *Client) GetArtifactPresignedURL(ctx context.Context, artifactID string) (string, error) {
	res, err := c.GetArtifactDownloadWithResponse(ctx, artifactID)
	if err != nil {
		return "", fmt.Errorf("artifact download request: %w", err)
	}
	if res.StatusCode() == 410 {
		return "", fmt.Errorf("artifact expired")
	}
	if res.StatusCode() >= 400 {
		return "", fmt.Errorf("artifact download failed: status %d: %s", res.StatusCode(), string(res.Body))
	}
	if res.JSON200 == nil || res.JSON200.PresignedUrl == nil {
		return "", fmt.Errorf("artifact download: empty presigned URL in response")
	}
	return *res.JSON200.PresignedUrl, nil
}

// BaseURL returns the base URL of the API server.
func (c *Client) BaseURL() string { return c.baseURL }

// HTTPClient returns the underlying HTTP client.
func (c *Client) HTTPClient() *http.Client { return c.httpClient }

// doRevoke calls POST /api/agent/token/revoke.
func (c *Client) doRevoke(ctx context.Context, _ string) error {
	res, err := c.AgentTokenRevokeWithResponse(ctx)
	if err != nil {
		return err
	}
	if res.StatusCode() >= 400 {
		return fmt.Errorf("revoke failed: status %d: %s", res.StatusCode(), string(res.Body))
	}
	return nil
}

// --- Helpers ---

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
