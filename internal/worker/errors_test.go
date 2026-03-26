package worker

import (
	"errors"
	"testing"
)

func TestErrPoolStopped(t *testing.T) {
	if ErrPoolStopped.Error() != "worker: pool is stopped" {
		t.Errorf("unexpected message: %q", ErrPoolStopped.Error())
	}
}

func TestErrNilJob(t *testing.T) {
	if ErrNilJob.Error() != "worker: job is nil" {
		t.Errorf("unexpected message: %q", ErrNilJob.Error())
	}
}

func TestSentinelErrors_Distinct(t *testing.T) {
	if errors.Is(ErrPoolStopped, ErrNilJob) {
		t.Error("ErrPoolStopped and ErrNilJob should be distinct")
	}
}

func TestPanicError_Error(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"string panic", "something broke", "worker: panic recovered: something broke"},
		{"int panic", 42, "worker: panic recovered: 42"},
		{"nil panic", nil, "worker: panic recovered: <nil>"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := &PanicError{Value: tc.value}
			if e.Error() != tc.want {
				t.Errorf("got %q, want %q", e.Error(), tc.want)
			}
		})
	}
}

func TestPanicError_ImplementsError(t *testing.T) {
	var err error = &PanicError{Value: "test"}
	if err.Error() == "" {
		t.Error("should implement error interface")
	}
}

func TestPanicError_ErrorsAs(t *testing.T) {
	err := &PanicError{Value: "crash"}
	var target *PanicError
	if !errors.As(err, &target) {
		t.Error("errors.As should find PanicError")
	}
	if target.Value != "crash" {
		t.Errorf("value: got %v", target.Value)
	}
}
