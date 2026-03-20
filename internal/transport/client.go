// Package transport provides HTTP client utilities with 401 retry support.
package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

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
// It also wires the registerFn and revokeFn callbacks into TokenManager.
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
	tm.SetRegisterFunc(c.doRegister)
	tm.SetConnectFunc(c.doConnect)
	tm.SetRevokeFunc(c.doRevoke)

	return c, nil
}

// doRegister calls POST /api/agent/register with the cluster token.
func (c *Client) doRegister(ctx context.Context, req auth.RegistrationRequest) (*auth.RegistrationResponse, error) {
	clusterToken := c.tokenManager.ClusterToken()
	res, err := c.AgentRegisterWithResponse(ctx, api.AgentRegisterRequest{
		ClusterToken: &clusterToken,
		AgentId:      strPtr(req.AgentID),
		Scanners:     req.Scanners,
		Hostname:     strPtr(req.Hostname),
		IpAddress:    strPtr(req.IPAddress),
		Version:      strPtr(req.Version),
		SdkVersion:   strPtr(req.SdkVersion),
	})
	if err != nil {
		return nil, err
	}
	if res.StatusCode() >= 400 {
		return nil, fmt.Errorf("registration failed: status %d: %s", res.StatusCode(), string(res.Body))
	}
	if res.JSON200 == nil {
		return nil, fmt.Errorf("registration failed: empty response")
	}
	return parseRegisterResult(res.JSON200), nil
}

// parseRegisterResult converts generated AgentRegisterResult to auth.RegistrationResponse.
func parseRegisterResult(r *api.AgentRegisterResult) *auth.RegistrationResponse {
	resp := &auth.RegistrationResponse{
		AgentID: derefStr(r.AgentId),
		Token:   derefStr(r.Token),
		System:  r.System != nil && *r.System,
	}
	if r.ExpiresAt != nil {
		resp.ExpiresAt = r.ExpiresAt.Format("2006-01-02T15:04:05Z")
	}
	if r.Cluster != nil {
		resp.ClusterInfo = parseClusterInfo(r.Cluster)
	}
	if r.Scanners != nil {
		for _, s := range *r.Scanners {
			resp.Scanners = append(resp.Scanners, parseScannerInfo(s))
		}
	}
	return resp
}

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
		RequestName: derefStr(s.RequestName),
		DisplayName: derefStr(s.DisplayName),
		System:      s.System != nil && *s.System,
	}
}

// strPtr returns a pointer to s, or nil if s is empty.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// derefStr safely dereferences a string pointer.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// doConnect calls POST /api/agent/token for lightweight token exchange.
func (c *Client) doConnect(ctx context.Context, req auth.ConnectRequest) (*auth.ConnectResponse, error) {
	clusterToken := c.tokenManager.ClusterToken()
	var scanners *[]string
	if len(req.Scanners) > 0 {
		scanners = &req.Scanners
	}
	res, err := c.CreateAgentTokenWithResponse(ctx, api.CreateAgentTokenRequest{
		ClusterToken: clusterToken,
		Scanners:     scanners,
	})
	if err != nil {
		return nil, err
	}
	if res.StatusCode() >= 400 {
		return nil, fmt.Errorf("connect failed: status %d: %s", res.StatusCode(), string(res.Body))
	}
	if res.JSON200 == nil {
		return nil, fmt.Errorf("connect failed: empty response")
	}
	return parseConnectResult(res.JSON200), nil
}

// parseConnectResult converts generated CreateAgentTokenResult to auth.ConnectResponse.
func parseConnectResult(r *api.CreateAgentTokenResult) *auth.ConnectResponse {
	resp := &auth.ConnectResponse{
		AgentID: derefStr(r.AgentId),
		Token:   derefStr(r.Token),
		System:  r.System != nil && *r.System,
	}
	if r.ExpiresAt != nil {
		resp.ExpiresAt = r.ExpiresAt.Format("2006-01-02T15:04:05Z")
	}
	if r.Cluster != nil {
		resp.ClusterInfo = parseClusterInfo(r.Cluster)
	}
	if r.Scanners != nil {
		for _, s := range *r.Scanners {
			resp.Scanners = append(resp.Scanners, parseScannerInfo(s))
		}
	}
	return resp
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

// artifactDownloadResponse is the JSON body from GET /api/artifact/{id}/download.
type artifactDownloadResponse struct {
	PresignedUrl string `json:"presigned_url"`
	ExpiresIn    int    `json:"expires_in"`
}

// GetArtifactPresignedURL calls GET /api/artifact/{artifactID}/download and returns the presigned URL.
func (c *Client) GetArtifactPresignedURL(ctx context.Context, artifactID string) (string, error) {
	rawURL := strings.TrimRight(c.baseURL, "/") + "/api/artifact/" + artifactID + "/download"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("build artifact download request: %w", err)
	}
	token := c.tokenManager.AgentToken()
	if token != "" {
		req.Header.Set("X-Token", token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("artifact download request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 410 {
		return "", fmt.Errorf("artifact has expired (410)")
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("artifact download failed: status %d: %s", resp.StatusCode, string(body))
	}

	var result artifactDownloadResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode artifact download response: %w", err)
	}
	if result.PresignedUrl == "" {
		return "", fmt.Errorf("artifact download response missing presigned_url")
	}
	return result.PresignedUrl, nil
}

// CheckAndRetry wraps an API call with 401 detection and re-registration retry.
// Use this for API calls that may receive 401 due to token expiry.
func (c *Client) CheckAndRetry(ctx context.Context, statusCode int, body []byte, callFn func() (int, []byte, error)) (int, []byte, error) {
	if statusCode != 401 {
		return statusCode, body, nil
	}

	// Token expired — re-register
	oldToken := c.tokenManager.AgentToken()
	if err := c.tokenManager.Reregister(ctx, oldToken); err != nil {
		return statusCode, body, fmt.Errorf("re-registration failed: %w", err)
	}

	// Retry once with new token
	retryStatus, retryBody, err := callFn()
	if err != nil {
		return 0, nil, err
	}
	if retryStatus == 401 {
		return retryStatus, retryBody, fmt.Errorf("authentication failed after re-registration")
	}

	return retryStatus, retryBody, nil
}
