package rediver

import (
	"errors"
	"fmt"
	"time"
)

// Sentinel errors for common failure scenarios.
var (
	// ErrJobNotFound indicates the requested job does not exist.
	ErrJobNotFound = errors.New("rediver: job not found")

	// ErrJobCancelled indicates the job was cancelled.
	ErrJobCancelled = errors.New("rediver: job cancelled")

	// ErrInvalidJob indicates the job data is invalid or malformed.
	ErrInvalidJob = errors.New("rediver: invalid job")

	// ErrNoJobAvailable indicates no pending jobs are available.
	// This is not necessarily an error in one-shot mode.
	ErrNoJobAvailable = errors.New("rediver: no job available")

	// ErrConnectionLost indicates the connection to the server was lost.
	ErrConnectionLost = errors.New("rediver: connection lost")

	// ErrAuthFailed indicates authentication with the server failed.
	ErrAuthFailed = errors.New("rediver: authentication failed")

	// ErrRateLimited indicates the server is rate limiting requests.
	ErrRateLimited = errors.New("rediver: rate limited")

	// ErrMaxRetries indicates the maximum retry attempts were exceeded.
	ErrMaxRetries = errors.New("rediver: max retries exceeded")

	// ErrInvalidConfig indicates the configuration is invalid.
	ErrInvalidConfig = errors.New("rediver: invalid configuration")

	// ErrClusterRevoked indicates the cluster token was revoked (HTTP 4xx during refresh).
	ErrClusterRevoked = errors.New("rediver: cluster token revoked")

	// ErrAlreadyRunning is returned when a lifecycle method is called on an Agent
	// that has already started. Agents are one-shot; create a new Agent to restart.
	ErrAlreadyRunning = errors.New("rediver: agent already running")
)

// JobError wraps an error with job context.
type JobError struct {
	JobID   string
	Message string
	Err     error
}

func (e *JobError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("job %s: %s: %v", e.JobID, e.Message, e.Err)
	}
	return fmt.Sprintf("job %s: %s", e.JobID, e.Message)
}

func (e *JobError) Unwrap() error {
	return e.Err
}

// APIError represents an error response from the Rediver API.
type APIError struct {
	StatusCode int
	Message    string
	Response   string // Raw response body for debugging
	Err        error
}

func (e *APIError) Error() string {
	if e.Response != "" {
		return fmt.Sprintf("rediver api: %d - %s", e.StatusCode, e.Response)
	}
	if e.Message != "" {
		return fmt.Sprintf("rediver api: %d - %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("rediver api: status %d", e.StatusCode)
}

func (e *APIError) Unwrap() error {
	return e.Err
}

// IsRetryable returns true if the error can be retried.
func (e *APIError) IsRetryable() bool {
	switch e.StatusCode {
	case 429, 502, 503, 504:
		return true
	default:
		return false
	}
}

// RetryableError indicates an error that triggered a retry attempt.
type RetryableError struct {
	Err        error
	StatusCode int
	Attempt    int
	MaxAttempt int
	NextRetry  time.Duration
}

func (e *RetryableError) Error() string {
	return fmt.Sprintf("retryable error (attempt %d/%d, retry in %v): %v",
		e.Attempt, e.MaxAttempt, e.NextRetry, e.Err)
}

func (e *RetryableError) Unwrap() error {
	return e.Err
}
