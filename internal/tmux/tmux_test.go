package tmux

import (
	"testing"

	"github.com/jinmu/eme/internal/runner"
)

// TestSwitchClient_UsesSwitchClientNotSelectWindow guards the fix for the bug
// where eme used `tmux select-window` to move the user to a session — which
// only changes a session's active window and never moves the client between
// sessions. The correct command is `tmux switch-client -t <session>:<window>`.
func TestSwitchClient_UsesSwitchClientNotSelectWindow(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("tmux", []string{"switch-client", "-t", "eme-proj:@7"}, "", "", nil)
	old := Runner
	Runner = mock
	defer func() { Runner = old }()

	if err := SwitchClient("eme-proj", "@7"); err != nil {
		t.Fatalf("SwitchClient returned error: %v", err)
	}

	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 tmux call, got %d: %+v", len(mock.Calls), mock.Calls)
	}
	got := mock.Calls[0]
	if got.Name != "tmux" {
		t.Fatalf("expected tmux, got %q", got.Name)
	}
	if got.Args[0] != "switch-client" {
		t.Fatalf("expected subcommand switch-client (not select-window), got %q", got.Args[0])
	}
	want := []string{"switch-client", "-t", "eme-proj:@7"}
	if len(got.Args) != len(want) {
		t.Fatalf("args mismatch: got %v want %v", got.Args, want)
	}
	for i := range want {
		if got.Args[i] != want[i] {
			t.Fatalf("arg %d: got %q want %q", i, got.Args[i], want[i])
		}
	}
}
