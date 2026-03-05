package rediver

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDefaultRetryPolicy(t *testing.T) {
	p := DefaultRetryPolicy()
	if p.MaxAttempts != 5 {
		t.Errorf("MaxAttempts: got %d, want 5", p.MaxAttempts)
	}
	if p.InitialBackoff != 1*time.Second {
		t.Errorf("InitialBackoff: got %v, want 1s", p.InitialBackoff)
	}
	if p.MaxBackoff != 60*time.Second {
		t.Errorf("MaxBackoff: got %v, want 60s", p.MaxBackoff)
	}
	if p.BackoffMultiplier != 2.0 {
		t.Errorf("BackoffMultiplier: got %f, want 2.0", p.BackoffMultiplier)
	}
	if !p.Jitter {
		t.Error("Jitter: got false, want true")
	}
	if len(p.RetryableStatusCodes) != 4 {
		t.Fatalf("RetryableStatusCodes: got %d codes, want 4", len(p.RetryableStatusCodes))
	}
}

func TestAggressiveRetryPolicy(t *testing.T) {
	p := AggressiveRetryPolicy()
	if p.MaxAttempts != 10 {
		t.Errorf("MaxAttempts: got %d, want 10", p.MaxAttempts)
	}
	if p.InitialBackoff != 2*time.Second {
		t.Errorf("InitialBackoff: got %v, want 2s", p.InitialBackoff)
	}
	if p.MaxBackoff != 120*time.Second {
		t.Errorf("MaxBackoff: got %v, want 120s", p.MaxBackoff)
	}
}

func TestNoRetry(t *testing.T) {
	p := NoRetry()
	if p.MaxAttempts != 1 {
		t.Errorf("MaxAttempts: got %d, want 1", p.MaxAttempts)
	}
	if len(p.RetryableStatusCodes) != 0 {
		t.Errorf("RetryableStatusCodes: got %v, want empty", p.RetryableStatusCodes)
	}
}

func TestIsRetryableStatus(t *testing.T) {
	p := DefaultRetryPolicy()

	retryable := []int{429, 502, 503, 504}
	for _, code := range retryable {
		if !p.IsRetryableStatus(code) {
			t.Errorf("expected %d to be retryable", code)
		}
	}

	notRetryable := []int{200, 400, 401, 404, 500}
	for _, code := range notRetryable {
		if p.IsRetryableStatus(code) {
			t.Errorf("expected %d to not be retryable", code)
		}
	}
}

func TestIsRetryableError(t *testing.T) {
	p := DefaultRetryPolicy()

	// nil error
	if p.IsRetryableError(nil) {
		t.Error("nil error should not be retryable")
	}

	// Retryable API error
	if !p.IsRetryableError(&APIError{StatusCode: 429}) {
		t.Error("429 APIError should be retryable")
	}

	// Non-retryable API error
	if p.IsRetryableError(&APIError{StatusCode: 400}) {
		t.Error("400 APIError should not be retryable")
	}

	// Network error
	if !p.IsRetryableError(&mockNetError{}) {
		t.Error("net.Error should be retryable")
	}

	// Generic error
	if p.IsRetryableError(errors.New("generic")) {
		t.Error("generic error should not be retryable")
	}
}

// mockNetError implements net.Error for testing.
type mockNetError struct{}

func (e *mockNetError) Error() string   { return "network error" }
func (e *mockNetError) Timeout() bool   { return true }
func (e *mockNetError) Temporary() bool { return true }

func TestBackoffDuration(t *testing.T) {
	p := RetryPolicy{
		InitialBackoff:    100 * time.Millisecond,
		MaxBackoff:        1 * time.Second,
		BackoffMultiplier: 2.0,
		Jitter:            false,
	}

	// attempt <= 0 returns 0
	if d := p.BackoffDuration(0); d != 0 {
		t.Errorf("attempt 0: got %v, want 0", d)
	}

	// attempt 1 = InitialBackoff
	if d := p.BackoffDuration(1); d != 100*time.Millisecond {
		t.Errorf("attempt 1: got %v, want 100ms", d)
	}

	// attempt 2 = 100ms * 2 = 200ms
	if d := p.BackoffDuration(2); d != 200*time.Millisecond {
		t.Errorf("attempt 2: got %v, want 200ms", d)
	}

	// attempt 3 = 100ms * 4 = 400ms
	if d := p.BackoffDuration(3); d != 400*time.Millisecond {
		t.Errorf("attempt 3: got %v, want 400ms", d)
	}

	// Capped at MaxBackoff
	if d := p.BackoffDuration(10); d != 1*time.Second {
		t.Errorf("attempt 10: got %v, want 1s (capped)", d)
	}
}

func TestBackoffDuration_WithJitter(t *testing.T) {
	p := RetryPolicy{
		InitialBackoff:    100 * time.Millisecond,
		MaxBackoff:        10 * time.Second,
		BackoffMultiplier: 2.0,
		Jitter:            true,
	}

	// With jitter, result should be >= base (jitter only adds)
	for i := 0; i < 50; i++ {
		d := p.BackoffDuration(1)
		if d < 100*time.Millisecond {
			t.Errorf("jitter produced value below base: %v", d)
		}
		// Max jitter is 25%, so max = 125ms
		if d > 125*time.Millisecond {
			t.Errorf("jitter exceeded 25%%: %v", d)
		}
	}
}

func TestRetrier_Success(t *testing.T) {
	r := newRetrier(NoRetry())
	calls := 0
	err := r.Do(context.Background(), func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestRetrier_RetryThenSuccess(t *testing.T) {
	p := RetryPolicy{
		MaxAttempts:          3,
		InitialBackoff:       1 * time.Millisecond,
		MaxBackoff:           10 * time.Millisecond,
		BackoffMultiplier:    1.0,
		RetryableStatusCodes: []int{500},
	}
	r := newRetrier(p)

	calls := 0
	err := r.Do(context.Background(), func() error {
		calls++
		if calls < 3 {
			return &APIError{StatusCode: 500}
		}
		return nil
	})
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestRetrier_NonRetryableError(t *testing.T) {
	p := RetryPolicy{
		MaxAttempts:          5,
		InitialBackoff:       1 * time.Millisecond,
		RetryableStatusCodes: []int{429},
	}
	r := newRetrier(p)

	calls := 0
	err := r.Do(context.Background(), func() error {
		calls++
		return &APIError{StatusCode: 400} // not retryable
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("expected 1 call (no retry), got %d", calls)
	}
}

func TestRetrier_MaxAttemptsExceeded(t *testing.T) {
	p := RetryPolicy{
		MaxAttempts:          3,
		InitialBackoff:       1 * time.Millisecond,
		MaxBackoff:           10 * time.Millisecond,
		BackoffMultiplier:    1.0,
		RetryableStatusCodes: []int{429},
	}
	r := newRetrier(p)

	calls := 0
	err := r.Do(context.Background(), func() error {
		calls++
		return &APIError{StatusCode: 429}
	})

	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}

	var retryErr *RetryableError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected *RetryableError, got %T", err)
	}
	if retryErr.Attempt != 3 || retryErr.MaxAttempt != 3 {
		t.Errorf("expected attempt 3/3, got %d/%d", retryErr.Attempt, retryErr.MaxAttempt)
	}
}

func TestRetrier_ContextCancelled(t *testing.T) {
	p := RetryPolicy{
		MaxAttempts:          10,
		InitialBackoff:       1 * time.Second,
		MaxBackoff:           10 * time.Second,
		BackoffMultiplier:    2.0,
		RetryableStatusCodes: []int{429},
	}
	r := newRetrier(p)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := r.Do(ctx, func() error {
		return &APIError{StatusCode: 429}
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestNewRetrier_ZeroMaxAttempts(t *testing.T) {
	r := newRetrier(RetryPolicy{MaxAttempts: 0})
	if r.policy.MaxAttempts != 1 {
		t.Errorf("expected normalized to 1, got %d", r.policy.MaxAttempts)
	}
}
