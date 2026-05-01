// Package connectclient provides Connect-protocol service clients for the
// Rediver agent plane, distributed via BSR (buf.build/rediver/api).
// Each Clients field is a generated Connect client interface backed by the
// supplied base URL and bearer transport.
package connectclient

import (
	"net/http"

	agentv1connect "buf.build/gen/go/rediver/api/connectrpc/go/agent/v1/agentv1connect"
)

// Clients bundles all Connect service clients tied to the same base URL and
// bearer transport. Construct via New.
type Clients struct {
	Token    agentv1connect.TokenServiceClient
	Agent    agentv1connect.AgentServiceClient
	Job      agentv1connect.JobServiceClient
	Artifact agentv1connect.ArtifactServiceClient
	Finding  agentv1connect.FindingServiceClient
	Asset    agentv1connect.AssetServiceClient
	Leak     agentv1connect.LeakServiceClient
}

// New constructs Clients with the given base URL and a transport that injects
// the X-Token header on each request when a non-empty token is provided via
// the SetToken callback. Pass empty string initially; call SetToken after
// generate-token succeeds.
//
// If httpClient is nil, http.DefaultClient is used as the base.
func New(baseURL string, tokenFn func() string, httpClient *http.Client) *Clients {
	base := http.DefaultClient
	if httpClient != nil {
		base = httpClient
	}
	wrapped := withAuthTransport(base, tokenFn)

	return &Clients{
		Token:    agentv1connect.NewTokenServiceClient(wrapped, baseURL),
		Agent:    agentv1connect.NewAgentServiceClient(wrapped, baseURL),
		Job:      agentv1connect.NewJobServiceClient(wrapped, baseURL),
		Artifact: agentv1connect.NewArtifactServiceClient(wrapped, baseURL),
		Finding:  agentv1connect.NewFindingServiceClient(wrapped, baseURL),
		Asset:    agentv1connect.NewAssetServiceClient(wrapped, baseURL),
		Leak:     agentv1connect.NewLeakServiceClient(wrapped, baseURL),
	}
}

// withAuthTransport wraps httpClient in a transport that injects X-Token on
// every request using the current value returned by tokenFn.
func withAuthTransport(base *http.Client, tokenFn func() string) *http.Client {
	baseTransport := base.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	wrapped := *base
	wrapped.Transport = &authTransport{base: baseTransport, tokenFn: tokenFn}
	return &wrapped
}

// authTransport injects X-Token from tokenFn into every outgoing request.
type authTransport struct {
	base    http.RoundTripper
	tokenFn func() string
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if token := t.tokenFn(); token != "" {
		req = req.Clone(req.Context())
		req.Header.Set("X-Token", token)
	}
	return t.base.RoundTrip(req)
}
