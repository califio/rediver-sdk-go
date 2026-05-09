package rediver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	scannerv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/scanner/v1"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/califio/rediver-sdk-go/internal/transport"
)

const (
	defaultEventChannelBuffer = 1024
	defaultEventFlushInterval = 200 * time.Millisecond
	defaultEventFlushBatch    = 50
	dropLogRateLimit          = 10 * time.Second
)

// eventSender abstracts the network call for testability.
type eventSender interface {
	SendJobEvents(ctx context.Context, jobID string, events []*scannerv1.JobEvent) error
}

// eventTransport owns the per-job event pipeline: channel + flush worker.
// Scanner code calls Submit; the worker batches and ships via eventSender.
type eventTransport struct {
	jobID         string
	logger        *slog.Logger
	sender        eventSender
	ch            chan Event
	channelBuffer int
	flushInterval time.Duration
	flushBatch    int
	sequence      atomic.Int64
	dropped       atomic.Int64
	lastDropLog   atomic.Int64
}

func newEventTransport(jobID string, sender eventSender, logger *slog.Logger,
	channelBuffer int, flushInterval time.Duration, flushBatch int) *eventTransport {
	if channelBuffer <= 0 {
		channelBuffer = defaultEventChannelBuffer
	}
	if flushInterval <= 0 {
		flushInterval = defaultEventFlushInterval
	}
	if flushBatch <= 0 {
		flushBatch = defaultEventFlushBatch
	}
	return &eventTransport{
		jobID:         jobID,
		logger:        logger,
		sender:        sender,
		ch:            make(chan Event, channelBuffer),
		channelBuffer: channelBuffer,
		flushInterval: flushInterval,
		flushBatch:    flushBatch,
	}
}

// Submit enqueues an event for delivery. Non-blocking — drops on full buffer
// and increments the dropped counter (rate-limited console warning).
func (t *eventTransport) Submit(ev Event) {
	ev.Sequence = t.sequence.Add(1)
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	select {
	case t.ch <- ev:
		// also write to console — formatter respects logger level
		emitConsole(context.Background(), t.logger, ev)
	default:
		dropped := t.dropped.Add(1)
		now := time.Now().UnixNano()
		last := t.lastDropLog.Load()
		if now-last >= int64(dropLogRateLimit) {
			if t.lastDropLog.CompareAndSwap(last, now) {
				t.logger.Warn("event channel full — dropping events",
					"job_id", t.jobID, "dropped_total", dropped)
			}
		}
	}
}

// Run drains the channel, flushing on tick OR batch size OR shutdown.
// Blocks until ctx is cancelled. After cancel, drains remaining buffered
// events with a 10s deadline.
func (t *eventTransport) Run(ctx context.Context) {
	ticker := time.NewTicker(t.flushInterval)
	defer ticker.Stop()

	batch := make([]Event, 0, t.flushBatch)

	flush := func(c context.Context) {
		if len(batch) == 0 {
			return
		}
		out := batch
		batch = make([]Event, 0, t.flushBatch)
		t.send(c, out)
	}

	for {
		select {
		case <-ctx.Done():
			finalCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			// Drain any remaining buffered events
			for {
				select {
				case ev := <-t.ch:
					batch = append(batch, ev)
					if len(batch) >= t.flushBatch {
						flush(finalCtx)
					}
				default:
					flush(finalCtx)
					return
				}
			}
		case <-ticker.C:
			flush(ctx)
		case ev := <-t.ch:
			batch = append(batch, ev)
			if len(batch) >= t.flushBatch {
				flush(ctx)
			}
		}
	}
}

func (t *eventTransport) send(ctx context.Context, events []Event) {
	proto, err := toProtoEvents(events)
	if err != nil {
		t.logger.Warn("encode events failed", "job_id", t.jobID, "error", err)
		return
	}
	if err := t.sender.SendJobEvents(ctx, t.jobID, proto); err != nil {
		t.logger.Warn("send events failed", "job_id", t.jobID, "error", err, "count", len(events))
	}
}

func toProtoEvents(events []Event) ([]*scannerv1.JobEvent, error) {
	out := make([]*scannerv1.JobEvent, 0, len(events))
	for _, ev := range events {
		ps, err := payloadToStruct(ev.Payload)
		if err != nil {
			return nil, fmt.Errorf("seq=%d: %w", ev.Sequence, err)
		}
		out = append(out, &scannerv1.JobEvent{
			Sequence:  ev.Sequence,
			Timestamp: timestamppb.New(ev.Timestamp),
			Type:      string(ev.Type),
			Payload:   ps,
		})
	}
	return out, nil
}

func payloadToStruct(payload any) (*structpb.Struct, error) {
	if payload == nil {
		return nil, nil
	}
	// Marshal via JSON round-trip to handle nested any/typed structs uniformly.
	m, err := payloadToMap(payload)
	if err != nil {
		return nil, err
	}
	return structpb.NewStruct(m)
}

func payloadToMap(payload any) (map[string]any, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}
	return m, nil
}

// agentEventSender is the production sender — calls AppendJobEvents.
type agentEventSender struct {
	client *transport.Client
}

func (s *agentEventSender) SendJobEvents(ctx context.Context, _ string, events []*scannerv1.JobEvent) error {
	// ctx carries job token (WithJobToken); job_id is resolved from JWT claim server-side.
	return s.client.AppendJobEvents(ctx, &scannerv1.AppendJobEventsRequest{
		Events: events,
	})
}
