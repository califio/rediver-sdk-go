package rediver

import (
	"context"
	"errors"
	"testing"
)

func TestAgent_DoubleRunReturnsError(t *testing.T) {
	t.Setenv("REDIVER_TOKEN", "tok")
	scanner := NewScanner("test", []TargetType{TargetTypeDomain},
		func(_ context.Context, _ Job, _ func(Result)) error { return nil })
	a, err := NewAgent("tok", scanner, WithServerURL("http://invalid.local:1"))
	if err != nil {
		t.Fatal(err)
	}

	// Force running flag without actually starting a session
	a.running.Store(true)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = a.Run(ctx)
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("Run: got %v, want ErrAlreadyRunning", err)
	}
	err = a.RunOnce(ctx)
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("RunOnce: got %v, want ErrAlreadyRunning", err)
	}
	err = a.Dispatch(ctx, func(_ context.Context, _ PulledJob) error { return nil })
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("Dispatch: got %v, want ErrAlreadyRunning", err)
	}
}

func TestAgent_DispatchNilHandler(t *testing.T) {
	t.Setenv("REDIVER_TOKEN", "tok")
	scanner := NewScanner("test", []TargetType{TargetTypeDomain},
		func(_ context.Context, _ Job, _ func(Result)) error { return nil })
	a, err := NewAgent("tok", scanner)
	if err != nil {
		t.Fatal(err)
	}
	err = a.Dispatch(context.Background(), nil)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("got %v, want ErrInvalidConfig", err)
	}
}
