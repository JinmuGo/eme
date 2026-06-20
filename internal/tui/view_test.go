package tui

import "testing"

func TestAgentStatusGlyphLabel(t *testing.T) {
	cases := []struct {
		s     AgentStatus
		glyph string
		label string
	}{
		{StatusWaiting, "●", "waiting"},
		{StatusWorking, "◐", "working"},
		{StatusExited, "○", "exited"},
		{StatusIdle, "·", "idle"},
	}
	for _, c := range cases {
		if got := c.s.Glyph(); got != c.glyph {
			t.Errorf("Glyph(%v) = %q, want %q", c.s, got, c.glyph)
		}
		if got := c.s.Label(); got != c.label {
			t.Errorf("Label(%v) = %q, want %q", c.s, got, c.label)
		}
	}
}

func TestAgentStatusNeedsAttention(t *testing.T) {
	want := map[AgentStatus]bool{
		StatusWaiting: true,
		StatusExited:  true,
		StatusWorking: false,
		StatusIdle:    false,
	}
	for s, w := range want {
		if got := s.NeedsAttention(); got != w {
			t.Errorf("NeedsAttention(%v) = %v, want %v", s, got, w)
		}
	}
}
