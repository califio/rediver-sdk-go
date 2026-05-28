package rediver

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// runDispatcher runs Dispatcher mode: heartbeat + poll loop, calling handler
// synchronously for each polled job. Supports both short-poll and long-poll
// via dispatchParams(). The handler decides whether to run work in a goroutine.
func (a *Agent) runDispatcher(ctx context.Context, handler JobHandler) error {
	a.drainCtx, a.cancelDrain = context.WithCancel(context.Background())
	defer a.cancelDrain()

	a.logger.Info("agent started (dispatcher mode)")

	go a.heartbeatLoop(a.drainCtx)

	waitSeconds, clientSleep := a.config.dispatchParams()
	backoff := time.Second

	for {
		if ctx.Err() != nil {
			a.logger.Info("dispatcher shutting down")
			a.cancelDrain()
			return nil
		}

		jobID, scanner, err := a.client.DoPollJob(ctx, waitSeconds)
		if err != nil {
			if errors.Is(err, ErrClusterRevoked) {
				return err
			}
			a.logger.Warn("poll error", "error", err)
			sleep := backoff
			backoff = minDuration(backoff*2, 30*time.Second)
			if !sleepOrCancel(ctx, sleep) {
				return nil
			}
			continue
		}
		backoff = time.Second

		if jobID == "" {
			if clientSleep > 0 {
				if !sleepOrCancel(ctx, clientSleep) {
					return nil
				}
			}
			continue
		}

		if herr := handler(ctx, PulledJob{JobID: jobID, Scanner: scanner}); herr != nil {
			a.logger.Error("dispatch handler failed", "job_id", jobID, "error", herr)
		}
	}
}

// Dispatch runs Dispatcher mode: persistent token, poll loop hands jobs to
// handler instead of executing them locally. The handler is called synchronously
// — use a goroutine inside the handler if concurrent dispatch is needed.
//
// Returns ErrAlreadyRunning if this Agent has already started.
func (a *Agent) Dispatch(ctx context.Context, handler JobHandler) error {
	if handler == nil {
		return fmt.Errorf("%w: handler must be non-nil", ErrInvalidConfig)
	}
	if !a.running.CompareAndSwap(false, true) {
		return ErrAlreadyRunning
	}
	if err := a.initSession(ctx, true, a.config.syncMetadataDispatcher); err != nil {
		return err
	}
	return a.runDispatcher(ctx, handler)
}
