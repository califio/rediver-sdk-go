// Package transport provides a Connect-protocol client with automatic X-Token
// injection and 401/Unauthenticated refresh via TokenManager.
package transport

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	agentv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/agent/v1"
	"connectrpc.com/connect"

	"github.com/califio/rediver-sdk-go/internal/auth"
	"github.com/califio/rediver-sdk-go/internal/connectclient"
)

// Client wraps the Connect service clients with TokenManager integration.
// 401/Unauthenticated is retried once after refreshing the agent token.
type Client struct {
	*connectclient.Clients
	tokenManager *auth.TokenManager
	baseURL      string
}

// NewClient creates a transport client with automatic token injection and 401 refresh.
func NewClient(baseURL string, tm *auth.TokenManager, httpClient *http.Client) (*Client, error) {
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	// Build base HTTP client with 401-retry transport
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

	clients := connectclient.New(baseURL, tm.AgentToken, authClient)

	c := &Client{
		Clients:      clients,
		tokenManager: tm,
		baseURL:      baseURL,
	}

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
	// Buffer body for potential retry (most SDK requests are small JSON/protobuf)
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

// DoPollJob calls PollJob RPC and returns (jobID, scanner, error).
// Returns ("", "", nil) when no job is available.
//
// waitSeconds > 0 enables long-poll: the server holds the request until a job
// becomes available or the deadline is reached. Pass 0 for legacy short-poll.
func (c *Client) DoPollJob(ctx context.Context, waitSeconds int32) (string, string, error) {
	if waitSeconds > 0 {
		var cancel context.CancelFunc
		// Add a 5-second buffer on top of the server wait to avoid premature
		// client-side timeout while the server is still holding the request.
		ctx, cancel = context.WithTimeout(ctx, time.Duration(waitSeconds+5)*time.Second)
		defer cancel()
	}
	resp, err := c.Job.PollJob(ctx, connect.NewRequest(&agentv1.PollJobRequest{
		WaitSeconds: waitSeconds,
	}))
	if err != nil {
		return "", "", fmt.Errorf("poll-job: %w", err)
	}
	msg := resp.Msg
	jobID := msg.GetJobId()
	scanner := msg.GetScanner()
	return jobID, scanner, nil
}

// DoGenerateToken calls GenerateToken RPC for per-scanner token exchange.
// This method is also used by authRetryTransport for 401 refresh.
func (c *Client) DoGenerateToken(ctx context.Context, req auth.GenerateTokenRequest) (*auth.GenerateTokenResponse, error) {
	protoReq := &agentv1.GenerateTokenRequest{
		ClusterToken: req.ClusterToken,
		Scanner:      strOptional(req.Scanner),
		Persistent:   req.Persistent,
		Hostname:     strOptional(req.Hostname),
		IpAddress:    strOptional(req.IPAddress),
		Version:      strOptional(req.Version),
		JobId:        req.JobId,
		AgentId:      req.AgentId,
	}
	resp, err := c.Token.GenerateToken(ctx, connect.NewRequest(protoReq))
	if err != nil {
		return nil, fmt.Errorf("generate-token: %w", err)
	}
	agentID := resp.Msg.GetAgentId()
	token := resp.Msg.GetToken()
	return &auth.GenerateTokenResponse{
		AgentId: &agentID,
		Token:   &token,
	}, nil
}

// AgentHeartbeat calls AgentService.Heartbeat RPC.
func (c *Client) AgentHeartbeat(ctx context.Context) error {
	_, err := c.Agent.Heartbeat(ctx, connect.NewRequest(&agentv1.HeartbeatRequest{}))
	if err != nil {
		return fmt.Errorf("agent heartbeat: %w", err)
	}
	return nil
}

// UpdateScanner calls AgentService.UpdateScanner RPC.
func (c *Client) UpdateScanner(ctx context.Context, req *agentv1.UpdateScannerRequest) error {
	_, err := c.Agent.UpdateScanner(ctx, connect.NewRequest(req))
	if err != nil {
		return fmt.Errorf("update scanner: %w", err)
	}
	return nil
}

// GetArtifactPresignedURL calls ArtifactService.GetArtifactDownload and returns
// the presigned download URL for the given artifactID.
func (c *Client) GetArtifactPresignedURL(ctx context.Context, artifactID string) (string, error) {
	resp, err := c.Artifact.GetArtifactDownload(ctx, connect.NewRequest(&agentv1.GetArtifactDownloadRequest{
		ArtifactId: artifactID,
	}))
	if err != nil {
		return "", fmt.Errorf("artifact download: %w", err)
	}
	return resp.Msg.GetPresignedUrl(), nil
}

// GetJobDetail calls JobService.GetJobDetail and returns the detail response.
func (c *Client) GetJobDetail(ctx context.Context, jobID string) (*agentv1.GetJobDetailResponse, error) {
	resp, err := c.Job.GetJobDetail(ctx, connect.NewRequest(&agentv1.GetJobDetailRequest{
		JobId: jobID,
	}))
	if err != nil {
		return nil, fmt.Errorf("get job detail: %w", err)
	}
	return resp.Msg, nil
}

// JobStart calls JobService.JobStart.
func (c *Client) JobStart(ctx context.Context, jobID string) error {
	_, err := c.Job.JobStart(ctx, connect.NewRequest(&agentv1.JobStartRequest{
		JobId: jobID,
	}))
	return err
}

// JobCompleted calls JobService.JobCompleted.
func (c *Client) JobCompleted(ctx context.Context, jobID string) error {
	_, err := c.Job.JobCompleted(ctx, connect.NewRequest(&agentv1.JobCompletedRequest{
		JobId: jobID,
	}))
	return err
}

// JobFailed calls JobService.JobFailed.
func (c *Client) JobFailed(ctx context.Context, jobID, description string) error {
	req := &agentv1.JobFailedRequest{JobId: jobID}
	if description != "" {
		req.Description = &description
	}
	_, err := c.Job.JobFailed(ctx, connect.NewRequest(req))
	return err
}

// JobHeartbeat calls JobService.JobHeartbeat.
func (c *Client) JobHeartbeat(ctx context.Context, jobID string) error {
	_, err := c.Job.JobHeartbeat(ctx, connect.NewRequest(&agentv1.JobHeartbeatRequest{
		JobId: jobID,
	}))
	return err
}

// PushAssets calls AssetService.PushAssets.
func (c *Client) PushAssets(ctx context.Context, req *agentv1.PushAssetsRequest) error {
	_, err := c.Asset.PushAssets(ctx, connect.NewRequest(req))
	if err != nil {
		return fmt.Errorf("push assets: %w", err)
	}
	return nil
}

// PushFindings calls FindingService.PushFindings.
func (c *Client) PushFindings(ctx context.Context, req *agentv1.PushFindingsRequest) error {
	_, err := c.Finding.PushFindings(ctx, connect.NewRequest(req))
	if err != nil {
		return fmt.Errorf("push findings: %w", err)
	}
	return nil
}

// CreateCiJob calls JobService.CreateCiJob and returns the response.
func (c *Client) CreateCiJob(ctx context.Context, req *agentv1.CreateCiJobRequest) (*agentv1.CreateCiJobResponse, error) {
	resp, err := c.Job.CreateCiJob(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, fmt.Errorf("create CI job: %w", err)
	}
	return resp.Msg, nil
}

// AppendJobEvents sends a batch of JobEvents to the backend.
func (c *Client) AppendJobEvents(ctx context.Context, req *agentv1.AppendJobEventsRequest) error {
	_, err := c.Job.AppendJobEvents(ctx, connect.NewRequest(req))
	if err != nil {
		return fmt.Errorf("append job events: %w", err)
	}
	return nil
}

// BaseURL returns the base URL of the API server.
func (c *Client) BaseURL() string { return c.baseURL }

// doRevoke calls TokenService.RevokeToken.
func (c *Client) doRevoke(ctx context.Context, _ string) error {
	_, err := c.Token.RevokeToken(ctx, connect.NewRequest(&agentv1.RevokeTokenRequest{}))
	if err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	return nil
}

// --- Helpers ---

// strOptional returns a pointer to s if non-empty, otherwise nil.
func strOptional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// strPtr returns a pointer to s. Used by tests.
func strPtr(s string) *string { return &s }
