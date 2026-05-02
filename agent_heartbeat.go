package rediver

import (
	"context"
	"time"
)

const (
	agentHeartbeatInterval = 60 * time.Second
	jobHeartbeatInterval   = 60 * time.Second
)

func (a *agent) heartbeatLoop(ctx context.Context) {
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

func (a *agent) jobHeartbeatLoop(ctx context.Context, jobID string) {
	ticker := time.NewTicker(jobHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.client.JobHeartbeat(ctx, jobID); err != nil {
				a.logger.Warn("job heartbeat failed", "job_id", jobID, "error", err)
			}
		}
	}
}
