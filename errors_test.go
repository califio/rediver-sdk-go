package rediver

import (
	"errors"
	"fmt"
	"io"
	"testing"
	"time"
)

func TestSentinelErrors_Identity(t *testing.T) {
	sentinels := []error{
		ErrJobNotFound, ErrJobCancelled, ErrInvalidJob, ErrNoJobAvailable,
		ErrConnectionLost, ErrAuthFailed, ErrRateLimited, ErrMaxRetries,
		ErrInvalidConfig, ErrReregistered,
	}
	for _, err := range sentinels {
		if !errors.Is(err, err) {
			t.Errorf("errors.Is(%v, itself) should be true", err)
		}
	}
}

func TestSentinelErrors_Distinct(t *testing.T) {
	if errors.Is(ErrJobNotFound, ErrJobCancelled) {
		t.Error("ErrJobNotFound should not match ErrJobCancelled")
	}
	if errors.Is(ErrAuthFailed, ErrInvalidConfig) {
		t.Error("ErrAuthFailed should not match ErrInvalidConfig")
	}
}

func TestJobError_Error_WithWrapped(t *testing.T) {
	err := &JobError{JobID: "j1", Message: "fail", Err: io.EOF}
	want := "job j1: fail: EOF"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestJobError_Error_WithoutWrapped(t *testing.T) {
	err := &JobError{JobID: "j1", Message: "fail"}
	want := "job j1: fail"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestJobError_Unwrap(t *testing.T) {
	inner := io.EOF
	err := &JobError{JobID: "j1", Message: "fail", Err: inner}
	if !errors.Is(err, io.EOF) {
		t.Error("JobError should unwrap to inner error")
	}
}

func TestAPIError_Error_WithResponse(t *testing.T) {
	err := &APIError{StatusCode: 400, Response: "bad request"}
	want := "rediver api: 400 - bad request"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestAPIError_Error_WithMessage(t *testing.T) {
	err := &APIError{StatusCode: 400, Message: "validation error"}
	want := "rediver api: 400 - validation error"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestAPIError_Error_StatusOnly(t *testing.T) {
	err := &APIError{StatusCode: 500}
	want := "rediver api: status 500"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestAPIError_Error_ResponsePriority(t *testing.T) {
	// Response takes priority over Message when both set
	err := &APIError{StatusCode: 400, Message: "msg", Response: "resp"}
	want := "rediver api: 400 - resp"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestAPIError_Unwrap(t *testing.T) {
	inner := io.EOF
	err := &APIError{StatusCode: 500, Err: inner}
	if !errors.Is(err, io.EOF) {
		t.Error("APIError should unwrap to inner error")
	}
}

func TestAPIError_IsRetryable(t *testing.T) {
	retryable := []int{429, 502, 503, 504}
	for _, code := range retryable {
		err := &APIError{StatusCode: code}
		if !err.IsRetryable() {
			t.Errorf("status %d should be retryable", code)
		}
	}

	notRetryable := []int{200, 400, 401, 404, 500}
	for _, code := range notRetryable {
		err := &APIError{StatusCode: code}
		if err.IsRetryable() {
			t.Errorf("status %d should not be retryable", code)
		}
	}
}

func TestRetryableError_Error(t *testing.T) {
	err := &RetryableError{
		Err:        io.EOF,
		Attempt:    3,
		MaxAttempt: 5,
		NextRetry:  2 * time.Second,
	}
	want := "retryable error (attempt 3/5, retry in 2s): EOF"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestRetryableError_Unwrap(t *testing.T) {
	inner := &APIError{StatusCode: 429}
	err := &RetryableError{Err: inner}
	var target *APIError
	if !errors.As(err, &target) {
		t.Error("RetryableError should unwrap to APIError")
	}
}

func TestErrorsAs_Wrapped(t *testing.T) {
	// Wrap APIError with fmt.Errorf
	apiErr := &APIError{StatusCode: 503, Message: "unavailable"}
	wrapped := fmt.Errorf("operation failed: %w", apiErr)

	var target *APIError
	if !errors.As(wrapped, &target) {
		t.Fatal("errors.As should find APIError through wrapping")
	}
	if target.StatusCode != 503 {
		t.Errorf("got status %d, want 503", target.StatusCode)
	}

	// Wrap JobError
	jobErr := &JobError{JobID: "j1", Message: "fail"}
	wrapped = fmt.Errorf("outer: %w", jobErr)

	var jobTarget *JobError
	if !errors.As(wrapped, &jobTarget) {
		t.Fatal("errors.As should find JobError through wrapping")
	}
	if jobTarget.JobID != "j1" {
		t.Errorf("got JobID %q, want j1", jobTarget.JobID)
	}
}
