package rediver

import (
	"context"
	"errors"
)

// runOnce runs Task mode: poll one job, execute it, revoke token, return.
// directJobID skips polling and executes that job directly when non-empty.
func (a *Agent) runOnce(ctx context.Context, directJobID string) error {
	var jobID string
	if directJobID != "" {
		jobID = directJobID
		a.logger.Info("direct job execution", "job_id", jobID)
	} else {
		var err error
		jobID, _, err = a.pullJob(ctx)
		if errors.Is(err, ErrNoJobAvailable) {
			a.logger.Info("no job available, exiting")
			_ = a.tokenManager.RevokeToken(ctx)
			return nil
		}
		if err != nil {
			_ = a.tokenManager.RevokeToken(ctx)
			return err
		}
	}

	execErr := a.executeJob(ctx, jobID)
	_ = a.tokenManager.RevokeToken(ctx)
	return execErr
}

// RunOnce runs Task mode: ephemeral token, polls one job (or executes the
// supplied jobID directly), revokes token, returns. When jobID is provided
// the token is bound to that job at gen time.
//
// Returns ErrAlreadyRunning if this Agent has already started.
func (a *Agent) RunOnce(ctx context.Context, jobID ...string) error {
	if !a.running.CompareAndSwap(false, true) {
		return ErrAlreadyRunning
	}
	directJobID := ""
	if len(jobID) > 0 {
		directJobID = jobID[0]
	}
	if err := a.initSession(ctx, false, false, directJobID); err != nil {
		return err
	}
	return a.runOnce(ctx, directJobID)
}
