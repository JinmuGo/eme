package tmux

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinmu/eme/internal/runner"
)

// TestTmux_PinsSocketWithDashL verifies that when a socket is pinned every tmux
// invocation is prefixed with `-L <socket>`, so eme always talks to one server.
func TestTmux_PinsSocketWithDashL(t *testing.T) {
	mock := runner.NewMock()
	oldRunner := Runner
	Runner = mock
	defer func() { Runner = oldRunner }()

	oldSocket := Socket
	Socket = "eme"
	defer func() { Socket = oldSocket }()

	mock.Set("tmux", []string{"-L", "eme", "switch-client", "-t", "proj:@7"}, "", "", nil)

	if err := SwitchClient("proj", "@7"); err != nil {
		t.Fatalf("SwitchClient returned error: %v", err)
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 tmux call, got %d: %+v", len(mock.Calls), mock.Calls)
	}
	want := []string{"-L", "eme", "switch-client", "-t", "proj:@7"}
	got := mock.Calls[0].Args
	if len(got) != len(want) {
		t.Fatalf("args mismatch: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg %d: got %q want %q", i, got[i], want[i])
		}
	}
}

// TestTmux_NoSocketLeavesArgsUntouched guards that the empty (test/legacy) socket
// value adds no -L flag, preserving ambient behavior.
func TestTmux_NoSocketLeavesArgsUntouched(t *testing.T) {
	oldSocket := Socket
	Socket = ""
	defer func() { Socket = oldSocket }()

	if got := withSocket([]string{"list-sessions"}); len(got) != 1 || got[0] != "list-sessions" {
		t.Fatalf("expected unmodified args, got %v", got)
	}
}

// TestPanesSnapshot_ParsesEmeStateAndLastPane guards two things: the @eme_state field
// is read into PaneInfo, and the LAST pane is NOT dropped when its @eme_state is empty
// — the outer TrimSpace strips that final line's trailing tab, leaving only 4 fields,
// so the parse must tolerate a missing 5th field.
func TestPanesSnapshot_ParsesEmeStateAndLastPane(t *testing.T) {
	oldRunner, oldSocket := Runner, Socket
	mock := runner.NewMock()
	Runner, Socket = mock, ""
	defer func() { Runner, Socket = oldRunner, oldSocket }()

	format := "#{window_id}\t#{pane_dead}\t#{pane_dead_status}\t#{pane_current_command}\t#{@eme_state}"
	// Last pane has an empty @eme_state + trailing tab, exactly the dropped-pane case.
	out := "@1\t0\t0\tnode\twaiting\n@2\t0\t0\tzsh\t\n"
	mock.Set("tmux", []string{"list-panes", "-a", "-F", format}, out, "", nil)

	snap, err := PanesSnapshot()
	if err != nil {
		t.Fatalf("PanesSnapshot: %v", err)
	}
	if a := snap["@1"]; a.EmeState != "waiting" || a.Command != "node" || a.Dead {
		t.Errorf("@1 = %+v, want {Command:node EmeState:waiting Dead:false}", a)
	}
	b, ok := snap["@2"]
	if !ok {
		t.Fatal("@2 (last pane, empty @eme_state) was dropped — trailing-tab parse regression")
	}
	if b.EmeState != "" || b.Command != "zsh" {
		t.Errorf(`@2 = %+v, want {Command:zsh EmeState:""}`, b)
	}
}

// TestClientOnManagedServer covers the switch-vs-attach decision: switch-client
// only moves the user when their client is attached to eme's pinned server.
func TestClientOnManagedServer(t *testing.T) {
	tmpdir := t.TempDir()
	t.Setenv("TMUX_TMPDIR", tmpdir)
	sockDir := filepath.Join(tmpdir, fmt.Sprintf("tmux-%d", os.Getuid()))
	if err := os.MkdirAll(sockDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	managed := filepath.Join(sockDir, "default")
	if err := os.WriteFile(managed, nil, 0o600); err != nil {
		t.Fatalf("write socket stand-in: %v", err)
	}
	other := filepath.Join(sockDir, "work")
	if err := os.WriteFile(other, nil, 0o600); err != nil {
		t.Fatalf("write socket stand-in: %v", err)
	}

	oldSocket := Socket
	defer func() { Socket = oldSocket }()

	cases := []struct {
		name   string
		socket string
		tmux   string
		want   bool
	}{
		{"not inside tmux", "default", "", false},
		{"pinned, client on managed server", "default", managed + ",123,0", true},
		{"pinned, client on a different server", "default", other + ",123,0", false},
		{"ambient (no pin), inside tmux", "", other + ",123,0", true},
		{"ambient (no pin), outside tmux", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			Socket = c.socket
			t.Setenv("TMUX", c.tmux)
			if got := ClientOnManagedServer(); got != c.want {
				t.Fatalf("ClientOnManagedServer() = %v, want %v", got, c.want)
			}
		})
	}
}

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

// TestCapturePane_TailAndTrim verifies the peek read trims a pane's trailing blank
// padding and returns only the last n lines, read-only.
func TestCapturePane_TailAndTrim(t *testing.T) {
	mock := runner.NewMock()
	oldRunner := Runner
	Runner = mock
	defer func() { Runner = oldRunner }()
	oldSocket := Socket
	Socket = ""
	defer func() { Socket = oldSocket }()

	out := "line1\nline2\nline3\nline4\nline5\nline6\n\n\n"
	mock.Set("tmux", []string{"capture-pane", "-p", "-t", "proj:@7"}, out, "", nil)

	got, err := CapturePane("proj", "@7", 3)
	if err != nil {
		t.Fatalf("CapturePane returned error: %v", err)
	}
	want := []string{"line4", "line5", "line6"}
	if len(got) != len(want) {
		t.Fatalf("got %d lines %v, want %v", len(got), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q want %q", i, got[i], want[i])
		}
	}
}

// TestCapturePane_EmptyPane: an all-blank pane peeks to zero lines, not a slice of
// empty strings.
func TestCapturePane_EmptyPane(t *testing.T) {
	mock := runner.NewMock()
	oldRunner := Runner
	Runner = mock
	defer func() { Runner = oldRunner }()
	oldSocket := Socket
	Socket = ""
	defer func() { Socket = oldSocket }()

	mock.Set("tmux", []string{"capture-pane", "-p", "-t", "proj:@7"}, "\n\n\n", "", nil)

	got, err := CapturePane("proj", "@7", 5)
	if err != nil {
		t.Fatalf("CapturePane returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d lines %v, want 0 for a blank pane", len(got), got)
	}
}
