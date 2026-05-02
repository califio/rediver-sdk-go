package rediver

import "testing"

func TestNewLog(t *testing.T) {
	ev := NewLog(LogLevelInfo, "scanning target=mblife.vn")
	if ev.Type != EventLog {
		t.Errorf("type: got %v, want EventLog", ev.Type)
	}
	p, ok := ev.Payload.(LogPayload)
	if !ok {
		t.Fatalf("payload: got %T, want LogPayload", ev.Payload)
	}
	if p.Level != LogLevelInfo {
		t.Errorf("level: got %v, want info", p.Level)
	}
	if p.Message != "scanning target=mblife.vn" {
		t.Errorf("message: got %q", p.Message)
	}
}

func TestNewToolUseStart(t *testing.T) {
	ev := NewToolUseStart("t1", "read_file", map[string]any{"path": "/etc/passwd"})
	if ev.Type != EventToolUseStart {
		t.Errorf("type: got %v, want %v", ev.Type, EventToolUseStart)
	}
	p, ok := ev.Payload.(ToolUseStartPayload)
	if !ok {
		t.Fatalf("payload: got %T", ev.Payload)
	}
	if p.ToolID != "t1" || p.Name != "read_file" {
		t.Errorf("got %+v", p)
	}
}

func TestNewToolUseEnd(t *testing.T) {
	ev := NewToolUseEnd("t1", "ok", false)
	if ev.Type != EventToolUseEnd {
		t.Errorf("type: got %v, want %v", ev.Type, EventToolUseEnd)
	}
	p, ok := ev.Payload.(ToolUseEndPayload)
	if !ok {
		t.Fatalf("payload: got %T", ev.Payload)
	}
	if p.ToolID != "t1" || p.IsError {
		t.Errorf("got %+v", p)
	}
}

func TestEventType_IsEphemeral(t *testing.T) {
	cases := []struct {
		t    EventType
		want bool
	}{
		{EventLog, false},
		{EventToolUseStart, false},
		{EventToolUseEnd, false},
		{EventThinkingDelta, true},
		{EventThinkingEnd, false},
		{EventTextDelta, true},
		{EventTextEnd, false},
	}
	for _, c := range cases {
		t.Run(string(c.t), func(t *testing.T) {
			if got := c.t.IsEphemeral(); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}
