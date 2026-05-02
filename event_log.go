package rediver

// LogPayload is the typed payload for EventLog events.
type LogPayload struct {
	Level   LogLevel `json:"level"`
	Message string   `json:"message"`
}

// NewLog creates a free-form log event. For structured/searchable activity
// (tool use, AI streaming), use the typed constructors instead.
func NewLog(level LogLevel, message string) Event {
	return Event{
		Type:    EventLog,
		Payload: LogPayload{Level: level, Message: message},
	}
}
