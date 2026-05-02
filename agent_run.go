package rediver

import (
	"context"
	"time"
)


// run starts the Worker mode lifecycle: heartbeat + poll loops.
func (a *Agent) run(ctx context.Context) error {
	a.drainCtx, a.cancelDrain = context.WithCancel(context.Background())
	defer a.cancelDrain()

	a.logger.Info("agent started (worker mode)")

	go a.heartbeatLoop(a.drainCtx)
	go a.pollLoop(ctx)

	<-ctx.Done()
	a.logger.Info("agent shutting down")

	done := make(chan struct{})
	go func() {
		a.pool.Shutdown()
		close(done)
	}()

	if a.config.shutdownTimeout > 0 {
		select {
		case <-done:
			a.logger.Info("all jobs completed")
		case <-time.After(a.config.shutdownTimeout):
			a.logger.Warn("shutdown timeout, forcing exit")
			a.cancelDrain()
			a.pool.ShutdownNow()
		}
	} else {
		<-done
		a.logger.Info("all jobs completed")
	}

	a.cancelDrain()
	return nil
}

// Run starts Worker mode: persistent token, long-running poll loop with worker
// pool, and agent heartbeat. Survives ctx cancellation up to shutdownTimeout
// to drain in-flight jobs.
//
// Returns ErrAlreadyRunning if this Agent has already started.
func (a *Agent) Run(ctx context.Context) error {
	if !a.running.CompareAndSwap(false, true) {
		return ErrAlreadyRunning
	}
	if err := a.initSession(ctx, true, true, ""); err != nil {
		return err
	}
	return a.run(ctx)
}
