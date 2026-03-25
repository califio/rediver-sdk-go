package rediver

import "context"

// PulledJob contains the information returned by GET /api/agent/job/poll.
// Passed to JobHandler in Dispatch mode.
type PulledJob struct {
	JobID   string
	Scanner string
}

// JobHandler is called when a job is pulled from the server in Dispatch mode.
// The handler receives a PulledJob and decides how to dispatch it externally
// (e.g., launch a K8s Job, enqueue to a task queue, etc.).
// Return nil on success. Returning an error causes the agent to report the job as
// failed so the server can re-queue it, then continues polling.
type JobHandler func(ctx context.Context, job PulledJob) error
