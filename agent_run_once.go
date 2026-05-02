package rediver

import (
	"context"
	"errors"
)

// runOnce runs Task mode: poll one job, execute it, revoke token, return.
func (a *Agent) runOnce(ctx context.Context) error {
	var jobID string
	if a.config.directJobID != "" {
		jobID = a.config.directJobID
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
