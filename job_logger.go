package rediver

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// slogLevelToLogLevel maps slog severity to rediver LogLevel.
// Defined here temporarily; moved to slog_adapter.go in Task 1.6.
func slogLevelToLogLevel(l slog.Level) LogLevel {
	switch {
	case l < slog.LevelInfo:
		return LogLevelDebug
	case l < slog.LevelWarn:
		return LogLevelInfo
	case l < slog.LevelError:
		return LogLevelWarn
	default:
		return LogLevelError
	}
}

// LogEntry represents a single log line from a scanner handler.
type LogEntry struct {
	Timestamp time.Time      `json:"timestamp"`
	Level     LogLevel       `json:"level"`
	Message   string         `json:"message"`
	Fields    map[string]any `json:"fields,omitempty"`
	JobID     string         `json:"job_id"`
	Scanner   string         `json:"scanner"`
}

const defaultMaxLogBuffer = 10 * 1024 * 1024 // 10MB

// jobBufferHandler is a slog.Handler that buffers log entries for transport
// to the backend. It is used as one of multiple handlers on a job logger.
type jobBufferHandler struct {
	mu         sync.Mutex
	entries    []LogEntry
	bufferSize int
	maxBuffer  int
	jobID      string
	scanner    string
}

func newJobBufferHandler(jobID, scanner string) *jobBufferHandler {
	return &jobBufferHandler{
		jobID:     jobID,
		scanner:   scanner,
		maxBuffer: defaultMaxLogBuffer,
	}
}

func (h *jobBufferHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

func (h *jobBufferHandler) Handle(_ context.Context, r slog.Record) error {
	entry := LogEntry{
		Timestamp: r.Time.UTC(),
		Level:     slogLevelToLogLevel(r.Level),
		Message:   r.Message,
		JobID:     h.jobID,
		Scanner:   h.scanner,
	}

	// Collect attrs into fields map.
	if r.NumAttrs() > 0 {
		entry.Fields = make(map[string]any, r.NumAttrs())
		r.Attrs(func(a slog.Attr) bool {
			entry.Fields[a.Key] = a.Value.Any()
			return true
		})
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	entrySize := 128 + len(r.Message)
	if h.bufferSize+entrySize > h.maxBuffer {
		return nil // drop when buffer full
	}
	h.entries = append(h.entries, entry)
	h.bufferSize += entrySize
	return nil
}

func (h *jobBufferHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Job buffer handler does not support pre-set attrs; return self.
	return h
}

func (h *jobBufferHandler) WithGroup(_ string) slog.Handler {
	// Job buffer handler does not support groups; return self.
	return h
}

// Drain returns all buffered entries and clears the buffer.
// Called by the transport layer to ship logs to the backend.
func (h *jobBufferHandler) Drain() []LogEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	entries := h.entries
	h.entries = nil
	h.bufferSize = 0
	return entries
}

// multiHandler fans out log records to multiple slog.Handlers.
type multiHandler struct {
	handlers []slog.Handler
}

func newMultiHandler(handlers ...slog.Handler) *multiHandler {
	return &multiHandler{handlers: handlers}
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range m.handlers {
		if h.Enabled(ctx, r.Level) {
			_ = h.Handle(ctx, r)
		}
	}
	return nil
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithAttrs(attrs)
	}
	return &multiHandler{handlers: handlers}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithGroup(name)
	}
	return &multiHandler{handlers: handlers}
}

// newJobLogger creates a *slog.Logger for a job with two handlers:
// 1. The agent's existing handler (console output with job context)
// 2. A buffer handler that captures entries for transport to backend
func newJobLogger(jobID, scanner string, agentLogger *slog.Logger) (*slog.Logger, *jobBufferHandler) {
	bufHandler := newJobBufferHandler(jobID, scanner)

	// Add job_id and scanner as default attrs on the agent handler.
	agentHandler := agentLogger.Handler().WithAttrs([]slog.Attr{
		slog.String("job_id", jobID),
		slog.String("scanner", scanner),
	})

	multi := newMultiHandler(agentHandler, bufHandler)
	return slog.New(multi), bufHandler
}
