package rediver

// ToolUseStartPayload is the typed payload for EventToolUseStart events.
type ToolUseStartPayload struct {
	ToolID string `json:"tool_id"`
	Name   string `json:"name"`
	Input  any    `json:"input"`
}

// ToolUseEndPayload is the typed payload for EventToolUseEnd events.
type ToolUseEndPayload struct {
	ToolID  string `json:"tool_id"`
	Output  any    `json:"output"`
	IsError bool   `json:"is_error"`
}

// NewToolUseStart signals an AI tool invocation has begun.
// toolID must match the corresponding NewToolUseEnd call.
func NewToolUseStart(toolID, name string, input any) Event {
	return Event{
		Type:    EventToolUseStart,
		Payload: ToolUseStartPayload{ToolID: toolID, Name: name, Input: input},
	}
}

// NewToolUseEnd signals an AI tool invocation has completed.
func NewToolUseEnd(toolID string, output any, isError bool) Event {
	return Event{
		Type:    EventToolUseEnd,
		Payload: ToolUseEndPayload{ToolID: toolID, Output: output, IsError: isError},
	}
}
