package worker

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testJob is a minimal Job implementation for testing.
type testJob struct {
	executeFn   func(ctx context.Context) error
	onEnqueue   func()
	onError     func(err error)
	onCompleted func()
}

func (j *testJob) Execute(ctx context.Context) error {
	if j.executeFn != nil {
		return j.executeFn(ctx)
	}
	return nil
}

func (j *testJob) OnEnqueue() {
	if j.onEnqueue != nil {
		j.onEnqueue()
	}
}

func (j *testJob) OnError(err error) {
	if j.onError != nil {
		j.onError(err)
	}
}

func (j *testJob) OnCompleted() {
	if j.onCompleted != nil {
		j.onCompleted()
	}
}

// --- NewPool ---

func TestNewPool_ValidSize(t *testing.T) {
	p := NewPool(4, 10)
	defer p.Shutdown()
	if p.Size() != 4 {
		t.Errorf("expected size 4, got %d", p.Size())
	}
}

func TestNewPool_ZeroSize(t *testing.T) {
	p := NewPool(0, 0)
	defer p.Shutdown()
	if p.Size() != 1 {
		t.Errorf("zero size should normalize to 1, got %d", p.Size())
	}
}

func TestNewPool_NegativeSize(t *testing.T) {
	p := NewPool(-5, -1)
	defer p.Shutdown()
	if p.Size() != 1 {
		t.Errorf("negative size should normalize to 1, got %d", p.Size())
	}
}

// --- Submit ---

func TestSubmit_NilJob(t *testing.T) {
	p := NewPool(1, 1)
	defer p.Shutdown()
	err := p.Submit(nil)
	if !errors.Is(err, ErrNilJob) {
		t.Errorf("expected ErrNilJob, got %v", err)
	}
}

func TestSubmit_Success(t *testing.T) {
	p := NewPool(1, 10)

	var completed atomic.Bool
	j := &testJob{
		onCompleted: func() { completed.Store(true) },
	}

	err := p.Submit(j)
	if err != nil {
		t.Fatal(err)
	}
	p.Shutdown()

	if !completed.Load() {
		t.Error("job should have completed")
	}
}

func TestSubmit_OnEnqueueCalled(t *testing.T) {
	p := NewPool(1, 10)

	var enqueued atomic.Bool
	j := &testJob{
		onEnqueue: func() { enqueued.Store(true) },
	}

	if err := p.Submit(j); err != nil {
		t.Fatal(err)
	}
	p.Shutdown()

	if !enqueued.Load() {
		t.Error("OnEnqueue should have been called")
	}
}

func TestSubmit_AfterShutdown(t *testing.T) {
	p := NewPool(1, 1)
	p.Shutdown()

	var errCalled atomic.Bool
	j := &testJob{
		onError: func(err error) { errCalled.Store(true) },
	}

	err := p.Submit(j)
	if !errors.Is(err, ErrPoolStopped) {
		t.Errorf("expected ErrPoolStopped, got %v", err)
	}
	if !errCalled.Load() {
		t.Error("OnError should be called with ErrPoolStopped")
	}
}

// --- Job execution ---

func TestPool_JobError(t *testing.T) {
	p := NewPool(1, 10)

	expectedErr := errors.New("job failed")
	var gotErr atomic.Value

	j := &testJob{
		executeFn: func(ctx context.Context) error { return expectedErr },
		onError:   func(err error) { gotErr.Store(err) },
	}

	if err := p.Submit(j); err != nil {
		t.Fatal(err)
	}
	p.Shutdown()

	if gotErr.Load() == nil {
		t.Fatal("OnError should have been called")
	}
	if gotErr.Load().(error).Error() != "job failed" {
		t.Errorf("unexpected error: %v", gotErr.Load())
	}
}

func TestPool_PanicRecovery(t *testing.T) {
	p := NewPool(1, 10)

	var gotErr atomic.Value

	j := &testJob{
		executeFn: func(ctx context.Context) error { panic("boom") },
		onError:   func(err error) { gotErr.Store(err) },
	}

	if err := p.Submit(j); err != nil {
		t.Fatal(err)
	}
	p.Shutdown()

	if gotErr.Load() == nil {
		t.Fatal("OnError should be called on panic")
	}
	var panicErr *PanicError
	if !errors.As(gotErr.Load().(error), &panicErr) {
		t.Fatalf("expected PanicError, got %T", gotErr.Load())
	}
	if panicErr.Value != "boom" {
		t.Errorf("panic value: got %v", panicErr.Value)
	}
}

// --- Concurrency ---

func TestPool_ConcurrentJobs(t *testing.T) {
	const poolSize = 4
	const jobCount = 20
	p := NewPool(poolSize, jobCount)

	var completed atomic.Int32

	for i := 0; i < jobCount; i++ {
		j := &testJob{
			executeFn:   func(ctx context.Context) error { time.Sleep(1 * time.Millisecond); return nil },
			onCompleted: func() { completed.Add(1) },
		}
		if err := p.Submit(j); err != nil {
			t.Fatal(err)
		}
	}
	p.Shutdown()

	if completed.Load() != jobCount {
		t.Errorf("expected %d completed, got %d", jobCount, completed.Load())
	}
}

func TestPool_MaxConcurrency(t *testing.T) {
	const poolSize = 2
	p := NewPool(poolSize, 10)

	var maxActive atomic.Int32
	var active atomic.Int32
	var mu sync.Mutex

	for i := 0; i < 10; i++ {
		j := &testJob{
			executeFn: func(ctx context.Context) error {
				cur := active.Add(1)
				mu.Lock()
				if cur > maxActive.Load() {
					maxActive.Store(cur)
				}
				mu.Unlock()
				time.Sleep(5 * time.Millisecond)
				active.Add(-1)
				return nil
			},
		}
		if err := p.Submit(j); err != nil {
			t.Fatal(err)
		}
	}
	p.Shutdown()

	if maxActive.Load() > poolSize {
		t.Errorf("max active workers (%d) exceeded pool size (%d)", maxActive.Load(), poolSize)
	}
}

// --- ActiveWorkers ---

func TestPool_ActiveWorkers_InitiallyZero(t *testing.T) {
	p := NewPool(2, 10)
	// Give dispatcher goroutine a moment to start
	time.Sleep(1 * time.Millisecond)
	if p.ActiveWorkers() != 0 {
		t.Errorf("expected 0 active, got %d", p.ActiveWorkers())
	}
	p.Shutdown()
}

// --- SetSize ---

func TestPool_SetSize_Increase(t *testing.T) {
	p := NewPool(1, 10)
	defer p.Shutdown()

	p.SetSize(4)
	if p.Size() != 4 {
		t.Errorf("expected size 4, got %d", p.Size())
	}
}

func TestPool_SetSize_Decrease(t *testing.T) {
	p := NewPool(4, 10)
	defer p.Shutdown()

	p.SetSize(2)
	if p.Size() != 2 {
		t.Errorf("expected size 2, got %d", p.Size())
	}
}

func TestPool_SetSize_Zero(t *testing.T) {
	p := NewPool(4, 10)
	defer p.Shutdown()

	p.SetSize(0)
	if p.Size() != 1 {
		t.Errorf("zero should normalize to 1, got %d", p.Size())
	}
}

// --- Shutdown ---

func TestShutdown_WaitsForCompletion(t *testing.T) {
	p := NewPool(1, 10)
	var completed atomic.Bool

	j := &testJob{
		executeFn: func(ctx context.Context) error {
			time.Sleep(50 * time.Millisecond)
			return nil
		},
		onCompleted: func() { completed.Store(true) },
	}
	if err := p.Submit(j); err != nil {
		t.Fatal(err)
	}

	p.Shutdown()
	if !completed.Load() {
		t.Error("Shutdown should wait for job completion")
	}
}

func TestShutdown_Idempotent(t *testing.T) {
	p := NewPool(1, 1)
	p.Shutdown()
	p.Shutdown() // Should not panic
}

// --- ShutdownNow ---

func TestShutdownNow_CancelsContext(t *testing.T) {
	p := NewPool(1, 10)
	var ctxCancelled atomic.Bool

	j := &testJob{
		executeFn: func(ctx context.Context) error {
			<-ctx.Done()
			ctxCancelled.Store(true)
			return ctx.Err()
		},
	}
	if err := p.Submit(j); err != nil {
		t.Fatal(err)
	}

	// Give job time to start
	time.Sleep(10 * time.Millisecond)
	p.ShutdownNow()

	if !ctxCancelled.Load() {
		t.Error("ShutdownNow should cancel the context")
	}
}

func TestShutdownNow_Idempotent(t *testing.T) {
	p := NewPool(1, 1)
	p.ShutdownNow()
	p.ShutdownNow() // Should not panic
}

// --- Mixed error and success ---

func TestPool_MixedErrorAndSuccess(t *testing.T) {
	p := NewPool(2, 10)
	var successes, failures atomic.Int32

	for i := 0; i < 10; i++ {
		idx := i
		j := &testJob{
			executeFn: func(ctx context.Context) error {
				if idx%2 == 0 {
					return errors.New("fail")
				}
				return nil
			},
			onError:     func(err error) { failures.Add(1) },
			onCompleted: func() { successes.Add(1) },
		}
		if err := p.Submit(j); err != nil {
			t.Fatal(err)
		}
	}
	p.Shutdown()

	if successes.Load() != 5 {
		t.Errorf("expected 5 successes, got %d", successes.Load())
	}
	if failures.Load() != 5 {
		t.Errorf("expected 5 failures, got %d", failures.Load())
	}
}
