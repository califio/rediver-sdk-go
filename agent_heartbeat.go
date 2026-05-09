package rediver

import (
	"context"
	"time"
)

const (
	agentHeartbeatInterval = 60 * time.Second
	jobHeartbeatInterval   = 60 * time.Second
)

func (a *Agent) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(agentHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.client.AgentHeartbeat(ctx); err != nil {
				a.logger.Warn("heartbeat failed", "error", err)
			}
		}
	}
}

func (a *Agent) jobHeartbeatLoop(ctx context.Context, jobID string) {
	ticker := time.NewTicker(jobHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// ctx carries the job token (WithJobToken) so JobHeartbeat routes via Bearer.
			if err := a.client.JobHeartbeat(ctx); err != nil {
				a.logger.Warn("job heartbeat failed", "job_id", jobID, "error", err)
			}
		}
	}
}
