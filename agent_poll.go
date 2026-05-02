package rediver

import (
	"context"
	"time"
)

// pollDoer abstracts DoPollJob so pollLoop can be unit-tested without a real
// transport.Client. Set agent.testPollDoer in tests; production code uses nil
// (falls back to agent.client).
type pollDoer interface {
	DoPollJob(ctx context.Context, waitSeconds int32) (string, string, error)
}

// pollLoop is the unified worker-mode poll loop.
// Behavior is parameterized by dispatchParams():
//
//	polling:      waitSeconds=0 + clientSleep=pollInterval (legacy short-poll)
//	long-polling: waitSeconds>0 + clientSleep=0 (server holds until job ready)
func (a *agent) pollLoop(ctx context.Context) {
	waitSeconds, clientSleep := a.config.dispatchParams()
	backoff := time.Second

	for {
		if ctx.Err() != nil {
			return
		}

		if a.pool.ActiveWorkers() >= a.config.maxConcurrency {
			if !sleepOrCancel(ctx, 500*time.Millisecond) {
				return
			}
			continue
		}

		poller := pollDoer(a.client)
		if a.testPollDoer != nil {
			poller = a.testPollDoer
		}
		jobID, _, err := poller.DoPollJob(ctx, waitSeconds)
		if err != nil {
			a.logger.Warn("poll error", "error", err)
			sleep := backoff
			backoff = minDuration(backoff*2, 30*time.Second)
			if !sleepOrCancel(ctx, sleep) {
				return
			}
			continue
		}
		backoff = time.Second

		if jobID != "" {
			err = a.pool.Submit(&agentPoolJob{a: a, ctx: a.drainCtx, jobID: jobID})
			if err != nil {
				a.logger.Error("submit job to pool failed", "job_id", jobID, "error", err)
				a.reportJobFailed(ctx, jobID, "worker pool full")
			}
			continue
		}

		if clientSleep > 0 {
			if !sleepOrCancel(ctx, clientSleep) {
				return
			}
		}
	}
}

// pullJob calls PollJob RPC. Returns ErrNoJobAvailable when no job is waiting.
// Used by runOnce (task mode) and runDispatcher.
func (a *agent) pullJob(ctx context.Context) (string, string, error) {
	jobID, scanner, err := a.client.DoPollJob(ctx, 0)
	if err != nil {
		return "", "", err
	}
	if jobID == "" {
		return "", "", ErrNoJobAvailable
	}
	return jobID, scanner, nil
}

func sleepOrCancel(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
