package rediver

import (
	"context"
	"fmt"

	"github.com/califio/rediver-sdk-go/internal/auth"
	"github.com/califio/rediver-sdk-go/internal/transport"
	"github.com/califio/rediver-sdk-go/internal/worker"
	"github.com/califio/rediver-sdk-go/utils"
)

// initSession generates a token for this Agent in the given mode and wires up
// transport client + worker pool. Called by each lifecycle method at start.
//
// persistent: true for Worker/Dispatcher (saved to Agents table, supports heartbeat),
//
//	false for Task/CI (ephemeral, revoked on exit).
//
// directJobID: when set (Task only), backend issues a token bound to that job.
// syncMetadata: push scanner config to backend (Worker always, Dispatcher opt-in).
func (a *Agent) initSession(ctx context.Context, persistent, syncMetadata bool, directJobID string) error {
	hostname := a.config.hostname
	if hostname == "" {
		hostname = utils.GetIPAddress()
	}

	genReq := auth.GenerateTokenRequest{
		ClusterToken: a.clusterToken,
		Scanner:      a.scannerName,
		Persistent:   persistent,
		Hostname:     hostname,
		IPAddress:    utils.GetIPAddress(),
		Version:      a.config.version,
	}
	if directJobID != "" {
		genReq.JobId = &directJobID
	}

	tm := auth.NewTokenManager(a.clusterToken)
	tm.SetGenReq(genReq)

	client, err := transport.NewClient(a.serverURL, tm, a.config.httpClient)
	if err != nil {
		return fmt.Errorf("create transport: %w", err)
	}

	var resp *auth.GenerateTokenResponse
	if err := a.retrier.Do(ctx, func() error {
		var genErr error
		resp, genErr = client.DoGenerateToken(ctx, genReq)
		return genErr
	}); err != nil {
		return fmt.Errorf("generate-token: %w", err)
	}

	agentID := derefStr(resp.AgentId)
	genReq.AgentId = &agentID
	tm.SetGenReq(genReq)
	tok := derefStr(resp.Token)
	tm.SetToken(tok)
	tm.SetAgentID(agentID)

	a.agentID = agentID
	a.tokenManager = tm
	a.client = client
	a.genReq = genReq
	a.token.Store(tok)
	a.logger = a.config.logger.With("scanner", a.scannerName, "agent_id", agentID)
	a.pool = worker.NewPool(a.config.maxConcurrency, a.config.maxConcurrency*2)

	if syncMetadata {
		a.syncScannerMetadata(ctx)
	}

	a.config.logger.Info("agent initialized",
		"scanner", a.scannerName,
		"agent_id", agentID,
		"persistent", persistent,
	)
	return nil
}
