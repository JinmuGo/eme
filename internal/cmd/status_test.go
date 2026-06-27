package cmd

import (
	"strings"
	"testing"

	"github.com/alderwork/eme/internal/runner"
)

// TestRenderSegment locks the segment formatting: dark when nothing needs you,
// glyph-led ✗/● with a count otherwise, and danger taking the slot over the beacon.
func TestRenderSegment(t *testing.T) {
	if got := renderSegment(0, 0); got != "" {
		t.Errorf("nothing needs you → %q, want empty (dark cockpit)", got)
	}
	if got := renderSegment(2, 0); !strings.Contains(got, "✗2") {
		t.Errorf("2 crashed → %q, want to contain ✗2", got)
	}
	if got := renderSegment(0, 3); !strings.Contains(got, "●3") {
		t.Errorf("3 waiting → %q, want to contain ●3", got)
	}
	// Danger beats the beacon: with both present, the crash takes the single slot.
	got := renderSegment(1, 5)
	if !strings.Contains(got, "✗1") {
		t.Errorf("crash present → %q, want ✗1 to take the slot", got)
	}
	if strings.Contains(got, "●") {
		t.Errorf("crash present → %q, beacon must not also show", got)
	}
}

// TestRenderSegmentClosesColorSpan guards the tmux color enhancement: any colored
// segment must close its #[fg=...] span so it never bleeds into the rest of the bar.
func TestRenderSegmentClosesColorSpan(t *testing.T) {
	for _, got := range []string{renderSegment(1, 0), renderSegment(0, 1)} {
		if strings.Contains(got, "#[fg=") && !strings.HasSuffix(got, "#[fg=default]") {
			t.Errorf("segment %q opens a color span without closing it", got)
		}
	}
}

// TestStatusSegment_EmptyWhenUnavailable locks the status-bar contract: a read
// failure degrades to an empty segment, never an error printed into the user's bar.
func TestStatusSegment_EmptyWhenUnavailable(t *testing.T) {
	tempState(t)
	prev := runner.Default
	runner.Default = runner.NewMock() // unstubbed → snapshot read fails
	t.Cleanup(func() { runner.Default = prev })

	if got := statusSegment(); got != "" {
		t.Errorf("statusSegment with no tmux = %q, want empty (degraded, no error)", got)
	}
}
