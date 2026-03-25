// Package transport provides HTTP client utilities with 401 retry support.
package transport

import (
	"context"
	"fmt"
	"net/http"

	"github.com/califio/rediver-sdk-go/internal/api"
	"github.com/califio/rediver-sdk-go/internal/auth"
)

// Client wraps the generated API client with TokenManager integration and 401 retry.
type Client struct {
	*api.ClientWithResponses
	tokenManager *auth.TokenManager
	httpClient   *http.Client
	baseURL      string
}

// NewClient creates a new transport client wired to TokenManager.
// It also wires the revokeFn and generateTokenFn callbacks into TokenManager.
func NewClient(baseURL string, tm *auth.TokenManager, httpClient *http.Client) (*Client, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	c := &Client{
		tokenManager: tm,
		httpClient:   httpClient,
		baseURL:      baseURL,
	}

	// Create API client with agent token injection
	apiClient, err := api.NewClientWithResponses(baseURL,
		api.WithHTTPClient(httpClient),
		api.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			token := tm.AgentToken()
			if token != "" {
				req.Header.Set("X-Token", token)
			}
			return nil
		}),
	)
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
	jobID := derefStr(res.JSON200.JobId)
	scanner := derefStr(res.JSON200.Scanner)
	return jobID, scanner, nil
}

// DoGenerateToken calls POST /api/agent/generate-token for per-scanner token exchange.
func (c *Client) DoGenerateToken(ctx context.Context, req auth.GenerateTokenRequest) (*auth.GenerateTokenResponse, error) {
	persistent := req.Persistent
	apiReq := api.GenerateAgentTokenRequest{
		ClusterToken: req.ClusterToken,
		AgentId:      req.AgentId, // nullable *string — set after first token for 401 refresh
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
	return parseGenerateTokenResult(res.JSON200), nil
}

// parseGenerateTokenResult converts generated GenerateAgentTokenResult to auth.GenerateTokenResponse.
// The generated struct only contains AgentId and Token fields.
func parseGenerateTokenResult(r *api.GenerateAgentTokenResult) *auth.GenerateTokenResponse {
	return &auth.GenerateTokenResponse{
		AgentID: derefStr(r.AgentId),
		Token:   derefStr(r.Token),
	}
}

// DoHeartbeatPing calls GET /api/agent/heartbeat (expects 204).
// Accepts a token parameter since per-scanner agents have independent tokens.
func (c *Client) DoHeartbeatPing(ctx context.Context, token string) error {
	// Use request editor to override the default TokenManager token with per-scanner token
	res, err := c.AgentHeartbeatPingWithResponse(ctx, func(ctx context.Context, req *http.Request) error {
		if token != "" {
			req.Header.Set("X-Token", token)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("heartbeat-ping request: %w", err)
	}
	if res.StatusCode() == 401 {
		return fmt.Errorf("heartbeat-ping: 401 unauthorized")
	}
	if res.StatusCode() >= 400 {
		return fmt.Errorf("heartbeat-ping failed: status %d", res.StatusCode())
	}
	return nil
}

// GetArtifactPresignedURL returns a presigned download URL for the given artifact ID.
// Not yet implemented in the generated API client — returns error for now.
func (c *Client) GetArtifactPresignedURL(ctx context.Context, artifactID string) (string, error) {
	return "", fmt.Errorf("artifact download not available: endpoint not implemented")
}

// BaseURL returns the base URL of the API server.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// HTTPClient returns the underlying HTTP client.
func (c *Client) HTTPClient() *http.Client {
	return c.httpClient
}

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

// UpdateScanner calls PATCH /api/agent/scanner using the generated client.
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

// CheckAndRetry wraps an API call with 401 detection and token-refresh retry.
func (c *Client) CheckAndRetry(ctx context.Context, statusCode int, body []byte, callFn func() (int, []byte, error)) (int, []byte, error) {
	if statusCode != 401 {
		return statusCode, body, nil
	}

	if err := c.tokenManager.GenerateToken(ctx); err != nil {
		return statusCode, body, fmt.Errorf("token refresh failed: %w", err)
	}

	retryStatus, retryBody, err := callFn()
	if err != nil {
		return 0, nil, err
	}
	if retryStatus == 401 {
		return retryStatus, retryBody, fmt.Errorf("authentication failed after re-registration")
	}

	return retryStatus, retryBody, nil
}

// --- Helpers ---

func parseClusterInfo(ci *api.AgentClusterInfo) auth.ClusterInfo {
	info := auth.ClusterInfo{
		ID:   derefStr(ci.Id),
		Name: derefStr(ci.Name),
	}
	if ci.AgentType != nil {
		info.AgentType = string(*ci.AgentType)
	}
	if ci.Tags != nil {
		info.Tags = *ci.Tags
	}
	if ci.AcceptUntaggedJobs != nil {
		info.AcceptUntaggedJobs = *ci.AcceptUntaggedJobs
	}
	if ci.MaxConcurrentJobs != nil {
		info.MaxConcurrentJobs = int(*ci.MaxConcurrentJobs)
	}
	return info
}

func parseScannerInfo(s api.ScannerInfo) auth.RegisteredScannerInfo {
	return auth.RegisteredScannerInfo{
		Name:        derefStr(s.Name),
		DisplayName: derefStr(s.DisplayName),
		System:      s.System != nil && *s.System,
	}
}

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
