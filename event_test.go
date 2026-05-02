package rediver

import "testing"

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
