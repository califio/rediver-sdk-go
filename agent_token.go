package rediver

import (
	"context"
	"fmt"

	scannerv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/scanner/v1"
	"github.com/califio/rediver-sdk-go/internal/auth"
	"github.com/califio/rediver-sdk-go/internal/transport"
	"github.com/califio/rediver-sdk-go/internal/worker"
	"github.com/califio/rediver-sdk-go/utils"
)

// initSession registers this Agent with the scanner runtime and wires up the
// transport client + worker pool. Called by each lifecycle method at start.
//
// persistent: true for Worker/Dispatcher (saved as AgentMachine row, supports
//
//	heartbeat); false for Task/CI (ephemeral, no heartbeat needed).
//
// syncMetadata: push scanner config to backend (Worker always, Dispatcher opt-in).
func (a *Agent) initSession(ctx context.Context, persistent, syncMetadata bool) error {
	hostname := a.config.hostname
	if hostname == "" {
		hostname = utils.GetIPAddress()
	}

	tm := auth.NewTokenManager(a.agentToken)

	client, err := transport.NewClient(a.serverURL, tm, a.config.httpClient)
	if err != nil {
		return fmt.Errorf("create transport: %w", err)
	}

	registerReq := &scannerv1.RegisterMachineRequest{
		RunnerId:  nil,
		Hostname:  strOptionalVal(hostname),
		IpAddress: strOptionalVal(utils.GetIPAddress()),
		Version:   strOptionalVal(a.config.version),
	}

	var runnerID string
	if err := a.retrier.Do(ctx, func() error {
		var registerErr error
		runnerID, registerErr = client.RegisterAgent(ctx, registerReq)
		return registerErr
	}); err != nil {
		return fmt.Errorf("register machine: %w", err)
	}

	tm.SetRunnerID(runnerID)
	a.runnerID = runnerID
	a.tokenManager = tm
	a.client = client
	a.logger = a.config.logger.With("scanner", a.scannerName, "runner_id", runnerID)
	a.pool = worker.NewPool(a.config.maxConcurrency, a.config.maxConcurrency*2)

	if syncMetadata {
		a.syncScannerMetadata(ctx)
	}

	a.config.logger.Info("agent initialized",
		"scanner", a.scannerName,
		"runner_id", runnerID,
		"persistent", persistent,
	)
	return nil
}

func strOptionalVal(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
