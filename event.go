package rediver

import "time"

// LogLevel represents log severity. Wire form is the lowercase string label.
type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// EventType discriminates Event payloads. Dotted namespace allows future
// subtypes without renaming siblings.
type EventType string

const (
	EventLog           EventType = "log"
	EventToolUseStart  EventType = "tool_use.start"
	EventToolUseEnd    EventType = "tool_use.end"
	EventThinkingDelta EventType = "thinking.delta"
	EventThinkingEnd   EventType = "thinking.end"
	EventTextDelta     EventType = "text.delta"
	EventTextEnd       EventType = "text.end"
)

// IsEphemeral returns true for delta event types that are not persisted to
// the durable store. Backend uses this to decide whether to write to job_logs.
func (t EventType) IsEphemeral() bool {
	return t == EventThinkingDelta || t == EventTextDelta
}

// Event is what scanners emit and what the SDK ships to the backend.
// Sequence and Timestamp are populated by the SDK at Emit time, not by the
// scanner.
type Event struct {
	Sequence  int64     // monotonic per-job, set by SDK
	Timestamp time.Time // SDK-assigned at Emit time
	Type      EventType
	Payload   any // type-specific concrete struct (see event_log.go etc.)
}
