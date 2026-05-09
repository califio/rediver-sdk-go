// Package connectclient provides Connect-protocol service clients for the
// Rediver agent plane, distributed via BSR (buf.build/rediver/api).
// Each Clients field is a generated Connect client interface backed by the
// supplied base URL and bearer transport.
package connectclient

import (
	"net/http"

	artifactv1connect "buf.build/gen/go/rediver/api/connectrpc/go/artifact/v1/artifactv1connect"
	"buf.build/gen/go/rediver/api/connectrpc/go/scanner/v1/scannerv1connect"
)

// Clients bundles all Connect service clients tied to the same base URL and
// bearer transport. Construct via New.
type Clients struct {
	ArtifactV1     artifactv1connect.ArtifactServiceClient
	Scanner        scannerv1connect.ScannerServiceClient
	ScannerJob     scannerv1connect.JobServiceClient
	ScannerFinding scannerv1connect.FindingServiceClient
	ScannerAsset   scannerv1connect.AssetServiceClient
	ScannerLeak    scannerv1connect.LeakServiceClient
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
		ArtifactV1:     artifactv1connect.NewArtifactServiceClient(wrapped, baseURL),
		Scanner:        scannerv1connect.NewScannerServiceClient(wrapped, baseURL),
		ScannerJob:     scannerv1connect.NewJobServiceClient(wrapped, baseURL),
		ScannerFinding: scannerv1connect.NewFindingServiceClient(wrapped, baseURL),
		ScannerAsset:   scannerv1connect.NewAssetServiceClient(wrapped, baseURL),
		ScannerLeak:    scannerv1connect.NewLeakServiceClient(wrapped, baseURL),
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
	if token := t.tokenFn(); token != "" && req.Header.Get("Authorization") == "" {
		req = req.Clone(req.Context())
		req.Header.Set("X-Token", token)
	}
	return t.base.RoundTrip(req)
}
