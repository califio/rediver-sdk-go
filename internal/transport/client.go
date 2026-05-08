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
	artifactv1 "github.com/califio/rediver-sdk-go/internal/gen/grpc/artifact/v1"
	authv1 "github.com/califio/rediver-sdk-go/internal/gen/grpc/auth/v1"
	commonv1 "github.com/califio/rediver-sdk-go/internal/gen/grpc/common/v1"
	scannerv1 "github.com/califio/rediver-sdk-go/internal/gen/grpc/scanner/v1"
)

type ArtifactDownloadInfo struct {
	PresignedURL        string
	EncryptionAlgorithm string
	EncryptionKey       string
}

// Client wraps the Connect service clients with TokenManager integration.
// 401/Unauthenticated is retried once after refreshing the agent token.
type Client struct {
	*connectclient.Clients
	tokenManager *auth.TokenManager
	baseURL      string
	jobTokens    map[string]string
	jobTokensMu  sync.Mutex
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
		jobTokens:    make(map[string]string),
	}

	// Wire callbacks into TokenManager (breaks circular dependency). The new
	// auth flow treats the configured token as the agent token, so revoke is a
	// compatibility no-op unless a caller installs its own revoke function.
	tm.SetGenerateTokenFunc(func(ctx context.Context, req auth.GenerateTokenRequest) (*auth.GenerateTokenResponse, error) {
		return c.DoGenerateToken(ctx, req)
	})

	return c, nil
}

// authRetryTransport injects X-Token header for agent-plane calls and retries
// once on 401 after refreshing when a refresh function is configured.
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
	if token != "" && req.Header.Get("Authorization") == "" {
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
	if req.Header.Get("Authorization") == "" {
		req.Header.Set("X-Token", t.tokenManager.AgentToken())
	}

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
	resp, err := c.Scanner.PollJob(ctx, connect.NewRequest(&scannerv1.PollJobRequest{
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

// RegisterAgent calls auth.v1.AuthService.RegisterAgent and returns the runner ID.
func (c *Client) RegisterAgent(ctx context.Context, req *authv1.RegisterAgentRequest) (string, error) {
	resp, err := c.Auth.RegisterAgent(ctx, connect.NewRequest(req))
	if err != nil {
		return "", fmt.Errorf("register agent: %w", err)
	}
	return resp.Msg.GetRunnerId(), nil
}

// CreateJobToken calls auth.v1.AuthService.CreateJobToken and caches the job JWT.
func (c *Client) CreateJobToken(ctx context.Context, jobID string) (string, error) {
	if jobID == "" {
		return "", fmt.Errorf("create job token: job ID is required")
	}
	c.jobTokensMu.Lock()
	if token := c.jobTokens[jobID]; token != "" {
		c.jobTokensMu.Unlock()
		return token, nil
	}
	c.jobTokensMu.Unlock()

	req := &authv1.CreateJobTokenRequest{
		JobId: jobID,
	}
	if runnerID := c.tokenManager.AgentID(); runnerID != "" {
		req.RunnerId = &runnerID
	}
	resp, err := c.Auth.CreateJobToken(ctx, connect.NewRequest(req))
	if err != nil {
		return "", fmt.Errorf("create job token: %w", err)
	}
	token := resp.Msg.GetToken()
	if token == "" {
		return "", fmt.Errorf("create job token: empty token")
	}

	c.jobTokensMu.Lock()
	c.jobTokens[jobID] = token
	c.jobTokensMu.Unlock()
	return token, nil
}

func (c *Client) jobBearer(ctx context.Context, jobID string) (string, error) {
	token, err := c.CreateJobToken(ctx, jobID)
	if err != nil {
		return "", err
	}
	return "Bearer " + token, nil
}

func withBearer[T any](req *connect.Request[T], bearer string) *connect.Request[T] {
	req.Header().Set("Authorization", bearer)
	return req
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
	_, err := c.Scanner.Heartbeat(ctx, connect.NewRequest(&scannerv1.HeartbeatRequest{
		RunnerId: c.tokenManager.AgentID(),
	}))
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

// GetArtifactDownload calls artifact.v1.ArtifactService.GetArtifactDownload and
// returns the presigned URL plus optional artifact encryption metadata.
func (c *Client) GetArtifactDownload(ctx context.Context, artifactID string) (*ArtifactDownloadInfo, error) {
	resp, err := c.ArtifactV1.GetArtifactDownload(ctx, connect.NewRequest(&artifactv1.GetArtifactDownloadRequest{
		ArtifactId: artifactID,
	}))
	if err != nil {
		return nil, fmt.Errorf("artifact download: %w", err)
	}
	out := &ArtifactDownloadInfo{
		PresignedURL:  resp.Msg.GetPresignedUrl(),
		EncryptionKey: resp.Msg.GetEncryptionKey(),
	}
	if resp.Msg.EncryptionAlgorithm != nil {
		switch resp.Msg.GetEncryptionAlgorithm() {
		case artifactv1.Algorithm_ALGORITHM_AES_256_GCM:
			out.EncryptionAlgorithm = "AES_256_GCM"
		default:
			out.EncryptionAlgorithm = resp.Msg.GetEncryptionAlgorithm().String()
		}
	}
	return out, nil
}

// GetArtifactPresignedURL is retained for callers that only need the URL.
func (c *Client) GetArtifactPresignedURL(ctx context.Context, artifactID string) (string, error) {
	info, err := c.GetArtifactDownload(ctx, artifactID)
	if err != nil {
		return "", err
	}
	return info.PresignedURL, nil
}

// GetJobDetail calls JobService.GetJobDetail and returns the detail response.
func (c *Client) GetJobDetail(ctx context.Context, jobID string) (*agentv1.GetJobDetailResponse, error) {
	bearer, err := c.jobBearer(ctx, jobID)
	if err != nil {
		return nil, err
	}
	resp, err := c.Scanner.GetJobDetail(ctx, withBearer(connect.NewRequest(&scannerv1.GetJobDetailRequest{
		JobId: jobID,
	}), bearer))
	if err != nil {
		return nil, fmt.Errorf("get job detail: %w", err)
	}
	return scannerDetailToAgentDetail(resp.Msg), nil
}

// JobStart calls JobService.JobStart.
func (c *Client) JobStart(ctx context.Context, jobID string) error {
	bearer, err := c.jobBearer(ctx, jobID)
	if err != nil {
		return err
	}
	_, err = c.Scanner.JobStart(ctx, withBearer(connect.NewRequest(&scannerv1.JobStartRequest{
		JobId: jobID,
	}), bearer))
	return err
}

// JobCompleted calls JobService.JobCompleted.
func (c *Client) JobCompleted(ctx context.Context, jobID string) error {
	bearer, err := c.jobBearer(ctx, jobID)
	if err != nil {
		return err
	}
	_, err = c.Scanner.JobCompleted(ctx, withBearer(connect.NewRequest(&scannerv1.JobCompletedRequest{
		JobId: jobID,
	}), bearer))
	return err
}

// JobFailed calls JobService.JobFailed.
func (c *Client) JobFailed(ctx context.Context, jobID, description string) error {
	req := &scannerv1.JobFailedRequest{JobId: jobID}
	if description != "" {
		req.Description = &description
	}
	bearer, err := c.jobBearer(ctx, jobID)
	if err != nil {
		return err
	}
	_, err = c.Scanner.JobFailed(ctx, withBearer(connect.NewRequest(req), bearer))
	return err
}

// JobHeartbeat calls JobService.JobHeartbeat.
func (c *Client) JobHeartbeat(ctx context.Context, jobID string) error {
	bearer, err := c.jobBearer(ctx, jobID)
	if err != nil {
		return err
	}
	_, err = c.Scanner.JobHeartbeat(ctx, withBearer(connect.NewRequest(&scannerv1.JobHeartbeatRequest{
		JobId: jobID,
	}), bearer))
	return err
}

// PushAssets calls AssetService.PushAssets.
func (c *Client) PushAssets(ctx context.Context, req *agentv1.PushAssetsRequest) error {
	bearer, err := c.jobBearer(ctx, req.GetJobId())
	if err != nil {
		return err
	}
	_, err = c.Asset.PushAssets(ctx, withBearer(connect.NewRequest(req), bearer))
	if err != nil {
		return fmt.Errorf("push assets: %w", err)
	}
	return nil
}

// PushFindings calls FindingService.PushFindings.
func (c *Client) PushFindings(ctx context.Context, req *agentv1.PushFindingsRequest) error {
	bearer, err := c.jobBearer(ctx, req.GetJobId())
	if err != nil {
		return err
	}
	_, err = c.Finding.PushFindings(ctx, withBearer(connect.NewRequest(req), bearer))
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
	bearer, err := c.jobBearer(ctx, req.GetJobId())
	if err != nil {
		return err
	}
	_, err = c.Job.AppendJobEvents(ctx, withBearer(connect.NewRequest(req), bearer))
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

func scannerDetailToAgentDetail(in *scannerv1.GetJobDetailResponse) *agentv1.GetJobDetailResponse {
	if in == nil {
		return nil
	}
	out := &agentv1.GetJobDetailResponse{
		Id:             in.GetId(),
		Scanner:        in.GetScanner(),
		Version:        in.GetVersion(),
		TimeoutMinutes: in.GetTimeoutMinutes(),
		Retest:         in.GetRetest(),
		Params:         in.GetParams(),
	}
	if in.GetTarget() != nil {
		out.Target = scannerTargetToAgentTarget(in.GetTarget())
	}
	if in.GetIntegration() != nil {
		out.Integration = &agentv1.JobIntegration{
			CloudflareTokens: in.GetIntegration().GetCloudflareTokens(),
		}
	}
	return out
}

func scannerTargetToAgentTarget(in *scannerv1.JobTarget) *agentv1.JobTarget {
	out := &agentv1.JobTarget{}
	for _, d := range in.GetDomains() {
		out.Domains = append(out.Domains, &agentv1.DomainAsset{
			Id:    d.Id,
			Value: d.GetValue(),
			Cname: d.Cname,
			Ips:   d.GetIps(),
		})
	}
	for _, ip := range in.GetIps() {
		out.Ips = append(out.Ips, &agentv1.ValueAsset{
			Id:    ip.Id,
			Value: ip.GetValue(),
		})
	}
	for _, subnet := range in.GetSubnets() {
		out.Subnets = append(out.Subnets, &agentv1.ValueAsset{
			Id:    subnet.Id,
			Value: subnet.GetValue(),
		})
	}
	for _, svc := range in.GetServices() {
		out.Services = append(out.Services, &agentv1.ServiceAsset{
			Id:    svc.Id,
			Value: svc.GetValue(),
			Host:  svc.Host,
			Port:  svc.Port,
			Url:   svc.Url,
		})
	}
	if in.GetRepository() != nil {
		out.Repository = repositoryTargetToAgentContext(in.GetRepository())
	}
	return out
}

func repositoryTargetToAgentContext(in *commonv1.RepositoryTarget) *agentv1.RepositoryJobContext {
	ref := in.GetBranch()
	if ref == "" {
		ref = in.GetTag()
	}
	out := &agentv1.RepositoryJobContext{
		Url:           in.GetUrl(),
		Provider:      agentv1.GitProvider(in.GetProvider()),
		ArtifactId:    in.ArtifactId,
		Event:         agentv1.CiEvent(in.GetEvent()),
		Ref:           ref,
		Branch:        in.Branch,
		CommitSha:     in.CommitSha,
		BaseBranch:    in.BaseBranch,
		BaseCommitSha: in.BaseCommitSha,
		PrNumber:      in.PullRequestNumber,
	}
	return out
}
