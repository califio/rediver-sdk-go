// Package worker provides a concurrent worker pool for job execution.
package worker

import (
	"context"
	"sync"
	"sync/atomic"
)

// Job represents a unit of work to be executed by the pool.
type Job interface {
	// Execute performs the job's work. Returns an error if the job fails.
	Execute(ctx context.Context) error
	// OnEnqueue is called when the job is added to the queue.
	OnEnqueue()
	// OnError is called when Execute returns an error.
	OnError(err error)
	// OnCompleted is called when Execute completes successfully.
	OnCompleted()
}

// Pool manages concurrent job execution with a configurable number of workers.
type Pool struct {
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	taskQueue chan Job
	sem       chan struct{}
	poolSize  atomic.Int32
	active    atomic.Int32
	running   atomic.Bool
	mu        sync.RWMutex
}

// NewPool creates a new worker pool with the specified size and queue capacity.
func NewPool(size int, queueSize int) *Pool {
	if size <= 0 {
		size = 1
	}
	if queueSize <= 0 {
		queueSize = 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	p := &Pool{
		ctx:       ctx,
		cancel:    cancel,
		taskQueue: make(chan Job, queueSize),
		sem:       make(chan struct{}, size),
	}
	p.poolSize.Store(int32(size))
	p.running.Store(true)

	go p.dispatch()
	return p
}

// Submit adds a job to the pool's queue.
// Returns an error if the pool is stopped.
func (p *Pool) Submit(job Job) error {
	if job == nil {
		return ErrNilJob
	}
	if !p.running.Load() {
		job.OnError(ErrPoolStopped)
		return ErrPoolStopped
	}

	p.wg.Add(1)
	p.taskQueue <- job
	job.OnEnqueue()
	return nil
}

// SetSize dynamically adjusts the pool size.
func (p *Pool) SetSize(size int) {
	if size <= 0 {
		size = 1
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	oldSize := int(p.poolSize.Load())
	p.poolSize.Store(int32(size))

	// Adjust semaphore capacity
	if size > oldSize {
		// Increase capacity by creating a new semaphore
		newSem := make(chan struct{}, size)
		// Transfer existing tokens
		close(p.sem)
		p.sem = newSem
	}
	// For decrease, we let existing workers finish naturally
}

// Size returns the current pool size.
func (p *Pool) Size() int {
	return int(p.poolSize.Load())
}

// ActiveWorkers returns the number of currently executing jobs.
func (p *Pool) ActiveWorkers() int {
	return int(p.active.Load())
}

// Shutdown gracefully stops the pool, waiting for all jobs to complete.
func (p *Pool) Shutdown() {
	if !p.running.CompareAndSwap(true, false) {
		return // Already stopped
	}
	close(p.taskQueue)
	p.wg.Wait()
	p.cancel()
}

// ShutdownNow stops the pool immediately, cancelling in-progress jobs.
func (p *Pool) ShutdownNow() {
	if !p.running.CompareAndSwap(true, false) {
		return
	}
	p.cancel() // Cancel context first
	close(p.taskQueue)
	p.wg.Wait()
}

func (p *Pool) dispatch() {
	for job := range p.taskQueue {
		// Acquire semaphore slot
		p.sem <- struct{}{}
		p.active.Add(1)

		go func(j Job) {
			defer func() {
				<-p.sem // Release semaphore slot
				p.active.Add(-1)
				p.wg.Done()
			}()

			// Handle panics
			defer func() {
				if r := recover(); r != nil {
					j.OnError(&PanicError{Value: r})
				}
			}()

			err := j.Execute(p.ctx)
			if err != nil {
				j.OnError(err)
			} else {
				j.OnCompleted()
			}
		}(job)
	}
}
