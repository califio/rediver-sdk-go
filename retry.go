package rediver

import (
	"context"
	"math"
	"math/rand"
	"net"
	"time"
)

// RetryPolicy configures retry behavior for transient errors.
type RetryPolicy struct {
	// MaxAttempts is the maximum number of attempts (including the first).
	// Default: 5
	MaxAttempts int

	// InitialBackoff is the wait time before the first retry.
	// Default: 1s
	InitialBackoff time.Duration

	// MaxBackoff is the maximum wait time between retries.
	// Default: 60s
	MaxBackoff time.Duration

	// BackoffMultiplier is the multiplier for exponential backoff.
	// Default: 2.0
	BackoffMultiplier float64

	// Jitter adds randomness to backoff to prevent thundering herd.
	// Default: true
	Jitter bool

	// RetryableStatusCodes are HTTP status codes that trigger retry.
	// Default: [429, 502, 503, 504]
	RetryableStatusCodes []int
}

// DefaultRetryPolicy returns sensible defaults for production use.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts:       5,
		InitialBackoff:    1 * time.Second,
		MaxBackoff:        60 * time.Second,
		BackoffMultiplier: 2.0,
		Jitter:            true,
		RetryableStatusCodes: []int{
			429, // Too Many Requests
			502, // Bad Gateway
			503, // Service Unavailable
			504, // Gateway Timeout
		},
	}
}

// AggressiveRetryPolicy returns a policy with more retries for daemon mode.
func AggressiveRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts:       10,
		InitialBackoff:    2 * time.Second,
		MaxBackoff:        120 * time.Second,
		BackoffMultiplier: 2.0,
		Jitter:            true,
		RetryableStatusCodes: []int{
			429, 502, 503, 504,
		},
	}
}

// NoRetry returns a policy that disables retry (fail on first error).
func NoRetry() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 1,
	}
}

// IsRetryableStatus returns true if the status code is retryable.
func (p RetryPolicy) IsRetryableStatus(statusCode int) bool {
	for _, code := range p.RetryableStatusCodes {
		if code == statusCode {
			return true
		}
	}
	return false
}

// IsRetryableError returns true if the error is retryable.
func (p RetryPolicy) IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for API errors
	if apiErr, ok := err.(*APIError); ok {
		return p.IsRetryableStatus(apiErr.StatusCode)
	}

	// Check for network errors
	if _, ok := err.(net.Error); ok {
		return true
	}

	return false
}

// BackoffDuration calculates the backoff duration for a given attempt.
// Attempt is 1-indexed (first retry is attempt 1).
func (p RetryPolicy) BackoffDuration(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}

	backoff := float64(p.InitialBackoff) * math.Pow(p.BackoffMultiplier, float64(attempt-1))
	if backoff > float64(p.MaxBackoff) {
		backoff = float64(p.MaxBackoff)
	}

	if p.Jitter {
		// Add up to 25% jitter
		jitter := backoff * 0.25 * rand.Float64()
		backoff += jitter
	}

	return time.Duration(backoff)
}

// retrier handles retry logic.
type retrier struct {
	policy RetryPolicy
}

func newRetrier(policy RetryPolicy) *retrier {
	if policy.MaxAttempts <= 0 {
		policy.MaxAttempts = 1
	}
	return &retrier{policy: policy}
}

// Do executes the function with retry logic.
func (r *retrier) Do(ctx context.Context, fn func() error) error {
	var lastErr error

	for attempt := 1; attempt <= r.policy.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if !r.policy.IsRetryableError(err) {
			return err
		}

		// Don't sleep on the last attempt
		if attempt < r.policy.MaxAttempts {
			backoff := r.policy.BackoffDuration(attempt)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
	}

	return &RetryableError{
		Err:        lastErr,
		Attempt:    r.policy.MaxAttempts,
		MaxAttempt: r.policy.MaxAttempts,
	}
}
