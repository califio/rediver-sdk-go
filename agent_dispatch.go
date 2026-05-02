package rediver

import (
	"context"
	"errors"
	"sync"
	"time"
)

// runDispatcher runs Dispatcher mode: heartbeat + poll loops, calling handler instead of executing.
func (a *agent) runDispatcher(ctx context.Context, handler JobHandler) error {
	a.drainCtx, a.cancelDrain = context.WithCancel(context.Background())
	defer a.cancelDrain()

	a.logger.Info("agent started (dispatcher mode)")

	go a.heartbeatLoop(a.drainCtx)

	ticker := time.NewTicker(a.config.pollInterval)
	defer ticker.Stop()

	var wg sync.WaitGroup
	for {
		select {
		case <-ctx.Done():
			a.logger.Info("dispatcher shutting down, waiting for handlers")
			wg.Wait()
			a.cancelDrain()
			return nil
		case <-ticker.C:
			jobID, scanner, err := a.pullJob(ctx)
			if err != nil {
				if !errors.Is(err, ErrNoJobAvailable) {
					a.logger.Error("pull job failed", "error", err)
					if errors.Is(err, ErrClusterRevoked) {
						return err
					}
				}
				continue
			}
			if jobID == "" {
				continue
			}

			pulled := PulledJob{JobID: jobID, Scanner: scanner}
			wg.Add(1)
			go func() {
				defer wg.Done()
				if herr := handler(ctx, pulled); herr != nil {
					a.logger.Error("dispatch handler failed", "job_id", jobID, "error", herr)
				}
			}()
		}
	}
}
