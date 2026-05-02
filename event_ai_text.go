package rediver

// TextPayload carries the text body for both deltas and ends.
// Same shape, different EventType discriminator.
type TextPayload struct {
	Text string `json:"text"`
}

// NewTextDelta emits a streaming text chunk. Ephemeral — not persisted to DB,
// only broadcast to live SSE viewers via Redis. Scanner author should
// accumulate deltas and emit NewTextEnd once the block completes.
func NewTextDelta(text string) Event {
	return Event{Type: EventTextDelta, Payload: TextPayload{Text: text}}
}

// NewTextEnd emits the complete accumulated text of a finished block.
// Durable — persisted to job_logs for replay.
func NewTextEnd(fullText string) Event {
	return Event{Type: EventTextEnd, Payload: TextPayload{Text: fullText}}
}

// NewThinkingDelta emits a streaming thinking chunk (Anthropic extended
// thinking, OpenAI o1 reasoning summaries). Ephemeral.
func NewThinkingDelta(text string) Event {
	return Event{Type: EventThinkingDelta, Payload: TextPayload{Text: text}}
}

// NewThinkingEnd emits the complete accumulated thinking block. Durable.
func NewThinkingEnd(fullText string) Event {
	return Event{Type: EventThinkingEnd, Payload: TextPayload{Text: fullText}}
}
