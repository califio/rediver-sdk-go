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
