package rediver

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	agentv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/agent/v1"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/califio/rediver-sdk-go/internal/transport"
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

// logSender abstracts the network call for testability.
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
func (t *logTransport) Run(ctx context.Context) {
	ticker := time.NewTicker(logFlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			t.flush(ctx)
		case <-ctx.Done():
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

// agentLogSender sends log chunks to the backend via the Connect client.
type agentLogSender struct {
	client *transport.Client
}

func (s *agentLogSender) SendJobLogs(ctx context.Context, jobID string, sequence int, entries []LogEntry) error {
	protoEntries := make([]*structpb.Struct, 0, len(entries))
	for _, e := range entries {
		fields := map[string]interface{}{
			"level":     int64(e.Level),
			"message":   e.Message,
			"timestamp": e.Timestamp,
			"job_id":    e.JobID,
			"scanner":   e.Scanner,
		}
		for k, v := range e.Fields {
			fields[k] = v
		}
		s, err := structpb.NewStruct(fields)
		if err != nil {
			continue // skip malformed entries rather than blocking log upload
		}
		protoEntries = append(protoEntries, s)
	}

	req := &agentv1.AppendJobLogsRequest{
		JobId:    jobID,
		Sequence: int32(sequence),
		Entries:  protoEntries,
	}
	if err := s.client.AppendJobLogs(ctx, req); err != nil {
		return fmt.Errorf("log upload failed: %w", err)
	}
	return nil
}
