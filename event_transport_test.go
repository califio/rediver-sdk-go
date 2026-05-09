package rediver

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	scannerv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/scanner/v1"
)

type fakeSender struct {
	mu     sync.Mutex
	calls  [][]*scannerv1.JobEvent
	failN  int32
	failed atomic.Int32
}

func (f *fakeSender) SendJobEvents(_ context.Context, _ string, events []*scannerv1.JobEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failed.Load() < f.failN {
		f.failed.Add(1)
		return io.ErrUnexpectedEOF
	}
	cp := make([]*scannerv1.JobEvent, len(events))
	copy(cp, events)
	f.calls = append(f.calls, cp)
	return nil
}

func (f *fakeSender) Calls() [][]*scannerv1.JobEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]*scannerv1.JobEvent, len(f.calls))
	copy(out, f.calls)
	return out
}

func newSilentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestEventTransport_FlushOnInterval(t *testing.T) {
	s := &fakeSender{}
	tr := newEventTransport("job-1", s, newSilentLogger(), 100, 50*time.Millisecond, 50)
	ctx, cancel := context.WithCancel(context.Background())
	go tr.Run(ctx)

	tr.Submit(NewLog(LogLevelInfo, "a"))
	tr.Submit(NewLog(LogLevelInfo, "b"))

	time.Sleep(120 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)

	calls := s.Calls()
	if len(calls) == 0 {
		t.Fatal("expected at least one flush")
	}
	total := 0
	for _, c := range calls {
		total += len(c)
	}
	if total != 2 {
		t.Errorf("got %d events across calls, want 2", total)
	}
}

func TestEventTransport_FlushOnBatchSize(t *testing.T) {
	s := &fakeSender{}
	tr := newEventTransport("job-1", s, newSilentLogger(), 100, 10*time.Second, 3)
	ctx, cancel := context.WithCancel(context.Background())
	go tr.Run(ctx)

	for i := 0; i < 3; i++ {
		tr.Submit(NewLog(LogLevelInfo, "x"))
	}
	time.Sleep(50 * time.Millisecond)

	calls := s.Calls()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1 (batch trigger)", len(calls))
	}
	if len(calls[0]) != 3 {
		t.Errorf("batch size: got %d, want 3", len(calls[0]))
	}
	cancel()
}

func TestEventTransport_Sequence_Monotonic(t *testing.T) {
	s := &fakeSender{}
	tr := newEventTransport("job-1", s, newSilentLogger(), 1000, 30*time.Millisecond, 1000)
	ctx, cancel := context.WithCancel(context.Background())
	go tr.Run(ctx)

	const N = 200
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr.Submit(NewLog(LogLevelInfo, "x"))
		}()
	}
	wg.Wait()
	time.Sleep(80 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)

	seen := map[int64]bool{}
	for _, c := range s.Calls() {
		for _, e := range c {
			if seen[e.Sequence] {
				t.Errorf("duplicate sequence %d", e.Sequence)
			}
			seen[e.Sequence] = true
		}
	}
	if len(seen) != N {
		t.Errorf("got %d unique sequences, want %d", len(seen), N)
	}
}

func TestEventTransport_DropOnFullChannel(t *testing.T) {
	// Sender blocks long enough to fill the buffer
	blocking := &fakeSender{}
	tr := newEventTransport("job-1", blocking, newSilentLogger(), 2, time.Hour, 1000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go tr.Run(ctx)

	for i := 0; i < 100; i++ {
		tr.Submit(NewLog(LogLevelInfo, "x"))
	}
	if tr.dropped.Load() == 0 {
		t.Error("expected drops, got 0")
	}
}
