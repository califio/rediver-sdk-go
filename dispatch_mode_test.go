package rediver

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/califio/rediver-sdk-go/internal/worker"
)

// --- mock pollDoer ---

type mockPollDoer struct {
	fn func(ctx context.Context, waitSeconds int32) (string, string, error)
}

func (m *mockPollDoer) DoPollJob(ctx context.Context, waitSeconds int32) (string, string, error) {
	return m.fn(ctx, waitSeconds)
}

// --- Test 1: WithDispatchMode validation ---

func TestWithDispatchMode(t *testing.T) {
	t.Parallel()

	// Valid modes must not panic.
	for _, valid := range []DispatchMode{DispatchPolling, DispatchLongPolling} {
		cfg := defaultAgentConfig()
		WithDispatchMode(valid)(cfg)
		if cfg.dispatchMode != valid {
			t.Errorf("WithDispatchMode(%q): got %q, want %q", valid, cfg.dispatchMode, valid)
		}
	}

	// Invalid mode must panic.
	defer func() {
		if r := recover(); r == nil {
			t.Error("WithDispatchMode with invalid mode should panic")
		}
	}()
	cfg := defaultAgentConfig()
	WithDispatchMode("invalid-mode")(cfg)
}

// --- Test 2: WithLongPollWait validation ---

func TestWithLongPollWait(t *testing.T) {
	t.Parallel()

	validCases := []time.Duration{
		1 * time.Second,
		30 * time.Second,
		60 * time.Second,
	}
	for _, d := range validCases {
		cfg := defaultAgentConfig()
		WithLongPollWait(d)(cfg)
		if cfg.longPollWait != d {
			t.Errorf("WithLongPollWait(%v): got %v, want %v", d, cfg.longPollWait, d)
		}
	}

	// Invalid: 0 and negative are ignored (longPollWait stays at default).
	invalidCases := []time.Duration{
		0,
		-1 * time.Second,
		61 * time.Second, // > 60s
	}
	for _, d := range invalidCases {
		cfg := defaultAgentConfig()
		original := cfg.longPollWait
		WithLongPollWait(d)(cfg)
		if cfg.longPollWait != original {
			t.Errorf("WithLongPollWait(%v) should be ignored; got %v, want %v", d, cfg.longPollWait, original)
		}
	}
}

// --- Test 3: dispatchParams returns correct values ---

func TestDispatchParams(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name               string
		mode               DispatchMode
		pollInterval       time.Duration
		longPollWait       time.Duration
		wantWaitSeconds    int32
		wantClientSleep    time.Duration
	}{
		{
			name:            "polling returns (0, pollInterval)",
			mode:            DispatchPolling,
			pollInterval:    5 * time.Second,
			longPollWait:    30 * time.Second,
			wantWaitSeconds: 0,
			wantClientSleep: 5 * time.Second,
		},
		{
			name:            "long-polling returns (longPollWait_secs, 0)",
			mode:            DispatchLongPolling,
			pollInterval:    5 * time.Second,
			longPollWait:    45 * time.Second,
			wantWaitSeconds: 45,
			wantClientSleep: 0,
		},
		{
			name:            "long-polling default 30s",
			mode:            DispatchLongPolling,
			pollInterval:    3 * time.Second,
			longPollWait:    30 * time.Second,
			wantWaitSeconds: 30,
			wantClientSleep: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := defaultAgentConfig()
			cfg.dispatchMode = tc.mode
			cfg.pollInterval = tc.pollInterval
			cfg.longPollWait = tc.longPollWait

			gotWait, gotSleep := cfg.dispatchParams()
			if gotWait != tc.wantWaitSeconds {
				t.Errorf("waitSeconds: got %d, want %d", gotWait, tc.wantWaitSeconds)
			}
			if gotSleep != tc.wantClientSleep {
				t.Errorf("clientSleep: got %v, want %v", gotSleep, tc.wantClientSleep)
			}
		})
	}
}

// --- Test 4: pollLoop call count differs by mode ---

// newTestAgent builds a minimal agent suitable for pollLoop testing.
// It does NOT call newAgent (which dials a real server); it constructs the struct directly.
// cfgFn is called after applying opts and allows direct field overrides (bypassing option guards).
func newTestAgent(t *testing.T, opts []Option, cfgFn func(*runnerConfig)) *agent {
	t.Helper()
	cfg := defaultAgentConfig()
	for _, o := range opts {
		o(cfg)
	}
	if cfgFn != nil {
		cfgFn(cfg)
	}
	pool := worker.NewPool(1, 2)
	t.Cleanup(pool.Shutdown)

	drainCtx, cancelDrain := context.WithCancel(context.Background())
	t.Cleanup(cancelDrain)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	return &agent{
		config:      cfg,
		pool:        pool,
		drainCtx:    drainCtx,
		cancelDrain: cancelDrain,
		logger:      logger,
	}
}

func TestPollLoop_DispatchModes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		opts             []Option
		// cfgFn allows direct config overrides that bypass option-level guards
		// (e.g. pollInterval minimum of 1s).
		cfgFn            func(*runnerConfig)
		wantWaitSeconds  int32
		minExpectedCalls int32
		// runWindow is how long we let pollLoop run before canceling.
		runWindow time.Duration
	}{
		{
			name: "polling sends waitSeconds=0 and is rate-limited by clientSleep",
			opts: []Option{WithDispatchMode(DispatchPolling)},
			// Set pollInterval to 50ms directly — WithPollInterval enforces ≥1s.
			cfgFn:            func(c *runnerConfig) { c.pollInterval = 50 * time.Millisecond },
			wantWaitSeconds:  0,
			minExpectedCalls: 3, // ≥3 calls in 300ms window with 50ms sleep
			runWindow:        300 * time.Millisecond,
		},
		{
			name:             "long-polling sends waitSeconds=30 and loops continuously (no client sleep)",
			opts:             []Option{WithDispatchMode(DispatchLongPolling)},
			cfgFn:            nil,
			wantWaitSeconds:  30,
			minExpectedCalls: 50, // mock returns instantly → many calls back-to-back
			runWindow:        250 * time.Millisecond,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32
			var wrongWait atomic.Bool

			mock := &mockPollDoer{
				fn: func(ctx context.Context, waitSeconds int32) (string, string, error) {
					if waitSeconds != tc.wantWaitSeconds {
						wrongWait.Store(true)
					}
					calls.Add(1)
					// Return empty — no job available.
					return "", "", nil
				},
			}

			a := newTestAgent(t, tc.opts, tc.cfgFn)
			a.testPollDoer = mock

			ctx, cancel := context.WithTimeout(context.Background(), tc.runWindow)
			defer cancel()

			// Run pollLoop in a goroutine; it exits when ctx is canceled.
			done := make(chan struct{})
			go func() {
				defer close(done)
				a.pollLoop(ctx)
			}()

			// Wait for the window to expire and pollLoop to return.
			<-done

			got := calls.Load()
			if got < tc.minExpectedCalls {
				t.Errorf("calls = %d, want ≥ %d", got, tc.minExpectedCalls)
			}
			if wrongWait.Load() {
				t.Errorf("DoPollJob received wrong waitSeconds (expected %d)", tc.wantWaitSeconds)
			}
		})
	}
}
