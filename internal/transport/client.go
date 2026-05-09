// Package transport provides a Connect-protocol client with automatic token
// injection. Agent-plane calls use X-Token; job-plane calls use
// Authorization: Bearer <jobToken> sourced from context (WithJobToken).
package transport

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"connectrpc.com/connect"

	artifactv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/artifact/v1"
	scannerv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/scanner/v1"
	"github.com/califio/rediver-sdk-go/internal/auth"
	"github.com/califio/rediver-sdk-go/internal/connectclient"
	"google.golang.org/protobuf/proto"
)

// jobTokenKey is the context key for per-job JWT tokens.
type jobTokenKey struct{}

// WithJobToken tags ctx with a JWT job token. Transport injects it as
// Authorization: Bearer for any outgoing request whose context carries it.
// Each concurrent job MUST derive its own ctx — context values are immutable,
// so distinct jobs cannot interfere.
func WithJobToken(ctx context.Context, jwt string) context.Context {
	return context.WithValue(ctx, jobTokenKey{}, jwt)
}

func jobTokenFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(jobTokenKey{}).(string)
	return v
}

// ArtifactDownloadInfo holds presigned URL + optional client-side decryption metadata.
type ArtifactDownloadInfo struct {
	PresignedURL        string
	EncryptionAlgorithm string
	EncryptionKey       string
}

// Client wraps the Connect service clients with TokenManager integration.
// Agent-plane RPCs use X-Token; job-plane RPCs use Authorization: Bearer
// sourced from the call's context via WithJobToken.
type Client struct {
	*connectclient.Clients
	tokenManager *auth.TokenManager
	baseURL      string
}

// NewClient creates a transport client with automatic token injection.
func NewClient(baseURL string, tm *auth.TokenManager, httpClient *http.Client) (*Client, error) {
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	base := httpClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	authClient := &http.Client{
		Timeout: httpClient.Timeout,
		Transport: &authTransport{
			base: base,
			tm:   tm,
		},
	}

	// Pass a no-op tokenFn to connectclient — all auth header injection is
	// handled by authTransport above (context-keyed routing). The connectclient
	// layer must not add a second X-Token layer.
	clients := connectclient.New(baseURL, func() string { return "" }, authClient)

	return &Client{
		Clients:      clients,
		tokenManager: tm,
		baseURL:      baseURL,
	}, nil
}

// authTransport injects the appropriate auth header on every outgoing request:
//   - If the request context carries a job token (WithJobToken), sets Authorization: Bearer <jwt>.
//   - Otherwise, sets X-Token to the agent token for agent-plane calls.
//
// There is no 401-retry — the agent token is persistent/config-managed and
// job tokens auto-invalidate server-side on JobCompleted/JobFailed.
type authTransport struct {
	base http.RoundTripper
	tm   *auth.TokenManager
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if jwt := jobTokenFromCtx(req.Context()); jwt != "" {
		req.Header.Set("Authorization", "Bearer "+jwt)
	} else if tok := t.tm.AgentToken(); tok != "" {
		req.Header.Set("X-Token", tok)
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
	resp, err := c.ScannerJob.PollJob(ctx, connect.NewRequest(&scannerv1.PollJobRequest{
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

// RegisterAgent calls scanner.v1.ScannerService.RegisterMachine and returns the runner ID.
func (c *Client) RegisterAgent(ctx context.Context, req *scannerv1.RegisterMachineRequest) (string, error) {
	resp, err := c.Scanner.RegisterMachine(ctx, connect.NewRequest(req))
	if err != nil {
		return "", fmt.Errorf("register agent: %w", err)
	}
	return resp.Msg.GetRunnerId(), nil
}

// CreateJobToken calls scanner.v1.ScannerService.CreateJobToken and returns the job JWT.
// This is an agent-plane call (uses X-Token, not job Bearer). Pass the runnerID returned
// by RegisterMachine so the backend can bind the token to this runner.
func (c *Client) CreateJobToken(ctx context.Context, jobID, runnerID string) (string, error) {
	if jobID == "" {
		return "", fmt.Errorf("create job token: job ID is required")
	}
	req := &scannerv1.CreateJobTokenRequest{
		JobId: jobID,
	}
	if runnerID != "" {
		req.RunnerId = &runnerID
	}
	resp, err := c.Scanner.CreateJobToken(ctx, connect.NewRequest(req))
	if err != nil {
		return "", fmt.Errorf("create job token: %w", err)
	}
	token := resp.Msg.GetToken()
	if token == "" {
		return "", fmt.Errorf("create job token: empty token")
	}
	return token, nil
}

// AgentHeartbeat calls scanner.v1.ScannerService.Heartbeat RPC (agent-plane, X-Token).
func (c *Client) AgentHeartbeat(ctx context.Context) error {
	_, err := c.Scanner.Heartbeat(ctx, connect.NewRequest(&scannerv1.HeartbeatRequest{
		RunnerId: c.tokenManager.RunnerID(),
	}))
	if err != nil {
		return fmt.Errorf("agent heartbeat: %w", err)
	}
	return nil
}

// UpdateScanner calls scanner.v1.ScannerService.UpdateScanner (agent-plane, X-Token).
func (c *Client) UpdateScanner(ctx context.Context, req *scannerv1.UpdateScannerRequest) error {
	_, err := c.Scanner.UpdateScanner(ctx, connect.NewRequest(req))
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

// GetJobDetail calls JobService.GetJobDetail. ctx must carry a job token
// (WithJobToken) so the request routes via Authorization: Bearer.
func (c *Client) GetJobDetail(ctx context.Context) (*scannerv1.GetJobDetailResponse, error) {
	resp, err := c.ScannerJob.GetJobDetail(ctx, connect.NewRequest(&scannerv1.GetJobDetailRequest{}))
	if err != nil {
		return nil, fmt.Errorf("get job detail: %w", err)
	}
	return resp.Msg, nil
}

// JobStart calls JobService.JobStart. ctx must carry a job token (WithJobToken).
func (c *Client) JobStart(ctx context.Context) error {
	_, err := c.ScannerJob.JobStart(ctx, connect.NewRequest(&scannerv1.JobStartRequest{}))
	return err
}

// JobCompleted calls JobService.JobCompleted. ctx must carry a job token (WithJobToken).
func (c *Client) JobCompleted(ctx context.Context) error {
	_, err := c.ScannerJob.JobCompleted(ctx, connect.NewRequest(&scannerv1.JobCompletedRequest{}))
	return err
}

// JobFailed calls JobService.JobFailed. ctx must carry a job token (WithJobToken).
func (c *Client) JobFailed(ctx context.Context, description string) error {
	req := &scannerv1.JobFailedRequest{}
	if description != "" {
		req.Description = &description
	}
	_, err := c.ScannerJob.JobFailed(ctx, connect.NewRequest(req))
	return err
}

// JobHeartbeat calls JobService.JobHeartbeat. ctx must carry a job token (WithJobToken).
func (c *Client) JobHeartbeat(ctx context.Context) error {
	_, err := c.ScannerJob.JobHeartbeat(ctx, connect.NewRequest(&scannerv1.JobHeartbeatRequest{}))
	return err
}

// PushAssets calls AssetService.PushAssets. ctx must carry a job token (WithJobToken).
func (c *Client) PushAssets(ctx context.Context, req *scannerv1.PushAssetsRequest) error {
	scannerReq := &scannerv1.PushAssetsRequest{}
	if err := transcodeProto(req, scannerReq); err != nil {
		return fmt.Errorf("push assets: %w", err)
	}
	_, err := c.ScannerAsset.PushAssets(ctx, connect.NewRequest(scannerReq))
	if err != nil {
		return fmt.Errorf("push assets: %w", err)
	}
	return nil
}

// PushFindings calls FindingService.PushFindings. ctx must carry a job token (WithJobToken).
func (c *Client) PushFindings(ctx context.Context, req *scannerv1.PushFindingsRequest) error {
	scannerReq := &scannerv1.PushFindingsRequest{}
	if err := transcodeProto(req, scannerReq); err != nil {
		return fmt.Errorf("push findings: %w", err)
	}
	_, err := c.ScannerFinding.PushFindings(ctx, connect.NewRequest(scannerReq))
	if err != nil {
		return fmt.Errorf("push findings: %w", err)
	}
	return nil
}

// CreateCiJob calls scanner.v1.JobService.CreateCiJob and returns the response
// (agent-plane call, uses X-Token).
func (c *Client) CreateCiJob(ctx context.Context, req *scannerv1.CreateCiJobRequest) (*scannerv1.CreateCiJobResponse, error) {
	resp, err := c.ScannerJob.CreateCiJob(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, fmt.Errorf("create CI job: %w", err)
	}
	return resp.Msg, nil
}

func transcodeProto(src proto.Message, dst proto.Message) error {
	b, err := proto.Marshal(src)
	if err != nil {
		return err
	}
	return proto.Unmarshal(b, dst)
}

// AppendJobEvents sends a batch of JobEvents to the backend.
// ctx must carry a job token (WithJobToken).
func (c *Client) AppendJobEvents(ctx context.Context, req *scannerv1.AppendJobEventsRequest) error {
	scannerReq := &scannerv1.AppendJobEventsRequest{}
	if err := transcodeProto(req, scannerReq); err != nil {
		return fmt.Errorf("append job events: %w", err)
	}
	_, err := c.ScannerJob.AppendJobEvents(ctx, connect.NewRequest(scannerReq))
	if err != nil {
		return fmt.Errorf("append job events: %w", err)
	}
	return nil
}

// BaseURL returns the base URL of the API server.
func (c *Client) BaseURL() string { return c.baseURL }

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
