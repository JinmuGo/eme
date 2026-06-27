package tmux

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/alderwork/eme/internal/runner"
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

// TestPanesSnapshot_ParsesEmeStateAndLastPane guards three things: window_activity and
// @eme_state are read into PaneInfo, and the LAST pane is NOT dropped when its hook tail is
// empty — the outer TrimSpace strips that final line's trailing tabs, leaving only the 5
// always-present fields (through window_activity), so the parse must tolerate a missing tail.
func TestPanesSnapshot_ParsesEmeStateAndLastPane(t *testing.T) {
	oldRunner, oldSocket := Runner, Socket
	mock := runner.NewMock()
	Runner, Socket = mock, ""
	defer func() { Runner, Socket = oldRunner, oldSocket }()

	format := "#{window_id}\t#{pane_dead}\t#{pane_dead_status}\t#{pane_current_command}\t#{window_activity}\t#{@eme_state}\t#{@eme_state_at}"
	// Last pane has an empty hook tail + trailing tabs, exactly the dropped-pane case.
	out := "@1\t0\t0\tnode\t1782353717\twaiting\t1750000000\n@2\t0\t0\tzsh\t1782353700\t\t\n"
	mock.Set("tmux", []string{"list-panes", "-a", "-F", format}, out, "", nil)

	snap, err := PanesSnapshot()
	if err != nil {
		t.Fatalf("PanesSnapshot: %v", err)
	}
	if a := snap["@1"]; a.EmeState != "waiting" || a.Command != "node" || a.Dead || a.Activity != 1782353717 {
		t.Errorf("@1 = %+v, want {Command:node EmeState:waiting Dead:false Activity:1782353717}", a)
	}
	b, ok := snap["@2"]
	if !ok {
		t.Fatal("@2 (last pane, empty hook tail) was dropped — trailing-tab parse regression")
	}
	if b.EmeState != "" || b.Command != "zsh" || b.Activity != 1782353700 {
		t.Errorf(`@2 = %+v, want {Command:zsh EmeState:"" Activity:1782353700}`, b)
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

// TestNewWindow_CreatesDetached guards that NewWindow passes -d so creating a
// worktree's window never steals the attached client's focus to the new (empty)
// window. Without -d, tmux makes the new window current and the client jumps to it
// the instant it is created — before createWorktree's agent picker runs — so the
// picker ends up in a now-backgrounded pane the user can neither see nor drive.
// eme moves the client deliberately afterward (maybeSwitchClient/switchToSession),
// mirroring NewSession, which is likewise detached.
func TestNewWindow_CreatesDetached(t *testing.T) {
	oldRunner, oldSocket := Runner, Socket
	mock := runner.NewMock()
	Runner, Socket = mock, ""
	defer func() { Runner, Socket = oldRunner, oldSocket }()

	want := []string{"new-window", "-d", "-t", "proj:", "-P", "-F", "#{window_id}", "-n", "feat", "-c", "/x/proj/feat"}
	mock.Set("tmux", want, "@9", "", nil)

	id, err := NewWindow("proj", "feat", "/x/proj/feat")
	if err != nil {
		t.Fatalf("NewWindow returned error: %v", err)
	}
	if id != "@9" {
		t.Fatalf("window id: got %q want @9", id)
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 tmux call, got %d: %+v", len(mock.Calls), mock.Calls)
	}
	if got := mock.Calls[0].Args; !slices.Contains(got, "-d") {
		t.Fatalf("NewWindow must pass -d so it never steals client focus; got args %v", got)
	}
}

// TestCapturePane_TailAndTrim verifies the capture trims a pane's trailing blank
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

// TestCapturePane_EmptyPane: an all-blank pane captures to zero lines, not a slice of
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

// TestParsePaneLine_ReadsActivityAndEmeState verifies the full 7-field row: window_activity
// (f[4]) into Activity, then the @eme_state / @eme_state_at hook tail into their fields.
func TestParsePaneLine_ReadsActivityAndEmeState(t *testing.T) {
	info, wid, ok := parsePaneLine("@5\t0\t0\tnode\t1782353717\tworking\t1750000000")
	if !ok {
		t.Fatal("expected a parsed pane")
	}
	if wid != "@5" {
		t.Errorf("window id = %q, want @5", wid)
	}
	if info.Activity != 1782353717 {
		t.Errorf("Activity = %d, want 1782353717", info.Activity)
	}
	if info.EmeState != "working" {
		t.Errorf("EmeState = %q, want working", info.EmeState)
	}
	if info.EmeStateAt != 1750000000 {
		t.Errorf("EmeStateAt = %d, want 1750000000", info.EmeStateAt)
	}
}

// TestParsePaneLine_UnhookedReadsActivity verifies the common un-hooked row: the hook tail is
// stripped as trailing empties, leaving 5 fields. Activity is still read; the hook fields stay
// zero-valued, so an un-hooked pane gets its silence signal without ever asserting a hook state.
func TestParsePaneLine_UnhookedReadsActivity(t *testing.T) {
	info, wid, ok := parsePaneLine("@5\t0\t0\tnode\t1782353717") // un-hooked: window_activity only
	if !ok || wid != "@5" {
		t.Fatalf("expected a parsed pane @5, got ok=%v wid=%q", ok, wid)
	}
	if info.Activity != 1782353717 {
		t.Errorf("Activity = %d, want 1782353717", info.Activity)
	}
	if info.EmeState != "" || info.EmeStateAt != 0 {
		t.Errorf("un-hooked pane must carry no hook state, got EmeState=%q EmeStateAt=%d", info.EmeState, info.EmeStateAt)
	}
}

// TestParsePaneLine_MissingTimestampIsZero verifies a hooked pane that pushed @eme_state but
// not @eme_state_at (6 fields): EmeStateAt parses to 0, Activity and EmeState still read.
func TestParsePaneLine_MissingTimestampIsZero(t *testing.T) {
	info, _, ok := parsePaneLine("@5\t0\t0\tnode\t1782353717\tworking") // 6 fields, no @eme_state_at
	if !ok || info.EmeStateAt != 0 {
		t.Fatalf("missing @eme_state_at must be 0, got ok=%v at=%d", ok, info.EmeStateAt)
	}
	if info.EmeState != "working" || info.Activity != 1782353717 {
		t.Errorf("EmeState=%q Activity=%d, want working / 1782353717", info.EmeState, info.Activity)
	}
}

func TestNewWindowCmd_RunsCommandDirectly(t *testing.T) {
	mock := runner.NewMock()
	prev := Runner
	Runner = mock
	defer func() { Runner = prev }()
	prevSock := Socket
	Socket = ""
	defer func() { Socket = prevSock }()

	want := []string{"new-window", "-d", "-t", "proj:", "-P", "-F", "#{window_id}",
		"-n", "__eme_caffeinate", "-c", "/code/proj/main",
		"/abs/eme", "caffeinate-daemon", "proj-1", "--mode", "auto"}
	mock.Set("tmux", want, "@9\n", "", nil)

	id, err := NewWindowCmd("proj", "__eme_caffeinate", "/code/proj/main",
		"/abs/eme", "caffeinate-daemon", "proj-1", "--mode", "auto")
	if err != nil {
		t.Fatalf("NewWindowCmd error: %v", err)
	}
	if id != "@9" {
		t.Fatalf("id = %q, want @9", id)
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.Calls))
	}
	if !slices.Equal(mock.Calls[0].Args, want) {
		t.Fatalf("args = %v, want %v", mock.Calls[0].Args, want)
	}
}
