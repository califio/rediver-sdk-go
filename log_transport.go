package rediver

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/califio/rediver-sdk-go/internal/api"
)

const (
	logFlushInterval = 5 * time.Second
)

// logTransport sends buffered log entries to the backend periodically.
type logTransport struct {
	jobID    string
	buffer   *jobBufferHandler
	sender   logSender
	logger   *slog.Logger // agent logger for transport-level warnings
	sequence int
	mu       sync.Mutex
}

// logSender abstracts the HTTP call for testability.
type logSender interface {
	SendJobLogs(ctx context.Context, jobID string, sequence int, entries []LogEntry) error
}

func newLogTransport(jobID string, buffer *jobBufferHandler, sender logSender, logger *slog.Logger) *logTransport {
	return &logTransport{
		jobID:  jobID,
		buffer: buffer,
		sender: sender,
		logger: logger,
	}
}

// Run starts the periodic flush loop. Blocks until ctx is cancelled.
// After cancellation, performs a final flush.
func (t *logTransport) Run(ctx context.Context) {
	ticker := time.NewTicker(logFlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			t.flush(ctx)
		case <-ctx.Done():
			// Final flush with background context
			finalCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			t.flush(finalCtx)
			cancel()
			return
		}
	}
}

func (t *logTransport) flush(ctx context.Context) {
	entries := t.buffer.Drain()
	if len(entries) == 0 {
		return
	}

	t.mu.Lock()
	seq := t.sequence
	t.sequence++
	t.mu.Unlock()

	if err := t.sender.SendJobLogs(ctx, t.jobID, seq, entries); err != nil {
		t.logger.Warn("failed to send job logs",
			"job_id", t.jobID, "sequence", seq, "error", err.Error())
	}
}

// apiLogAppender abstracts the generated API client for testability.
type apiLogAppender interface {
	AppendJobLogsWithResponse(ctx context.Context, body api.AppendJobLogsJSONRequestBody, reqEditors ...api.RequestEditorFn) (*api.AppendJobLogsResponse, error)
}

// agentLogSender sends log chunks to the backend via the generated API client.
type agentLogSender struct {
	client apiLogAppender
}

func (s *agentLogSender) SendJobLogs(ctx context.Context, jobID string, sequence int, entries []LogEntry) error {
	seq := int32(sequence)
	apiEntries := make([]api.JobLogEntry, len(entries))
	for i, e := range entries {
		apiEntries[i] = api.JobLogEntry{
			Level:     Ptr(int32(e.Level)),
			Message:   Ptr(e.Message),
			Timestamp: Ptr(e.Timestamp),
			JobId:     Ptr(e.JobID),
			Scanner:   Ptr(e.Scanner),
		}
		if len(e.Fields) > 0 {
			apiEntries[i].Fields = &e.Fields
		}
	}

	resp, err := s.client.AppendJobLogsWithResponse(ctx, api.AppendJobLogsRequest{
		JobId:    jobID,
		Sequence: &seq,
		Entries:  apiEntries,
	})
	if err != nil {
		return err
	}
	if resp.StatusCode() >= 400 {
		return fmt.Errorf("log upload failed: HTTP %d", resp.StatusCode())
	}
	return nil
}
