package worker

import (
	"errors"
	"fmt"
)

var (
	// ErrPoolStopped is returned when submitting to a stopped pool.
	ErrPoolStopped = errors.New("worker: pool is stopped")

	// ErrNilJob is returned when submitting a nil job.
	ErrNilJob = errors.New("worker: job is nil")
)

// PanicError wraps a recovered panic value.
type PanicError struct {
	Value any
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("worker: panic recovered: %v", e.Value)
}
