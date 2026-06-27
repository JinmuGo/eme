package cmd

import (
	"slices"
	"testing"
	"time"

	"github.com/alderwork/eme/internal/runner"
	"github.com/alderwork/eme/internal/state"
	"github.com/alderwork/eme/internal/tmux"
	"github.com/alderwork/eme/internal/tui"
)

func TestAnyWorking(t *testing.T) {
	if anyWorking(nil) {
		t.Fatal("nil → false")
	}
	if anyWorking([]tui.AgentStatus{tui.StatusIdle, tui.StatusExited}) {
		t.Fatal("no working → false")
	}
	if !anyWorking([]tui.AgentStatus{tui.StatusIdle, tui.StatusWorking}) {
		t.Fatal("one working → true")
	}
}

func TestShouldAssert(t *testing.T) {
	grace := 60 * time.Second
	if !shouldAssert(true, 0, grace) {
		t.Fatal("working → assert")
	}
	if !shouldAssert(false, 30*time.Second, grace) {
		t.Fatal("idle within grace → assert")
	}
	if shouldAssert(false, 90*time.Second, grace) {
		t.Fatal("idle past grace → release")
	}
	if shouldAssert(false, 10*time.Second, 0) {
		t.Fatal("zero grace, idle → release")
	}
}

func TestShouldAssert_GraceBoundary(t *testing.T) {
	grace := 60 * time.Second
	// Exactly at grace: sinceLast == grace uses <, so it must release (false).
	if shouldAssert(false, grace, grace) {
		t.Fatal("sinceLast == grace must release (< not <=)")
	}
}

func TestNormalizeMode(t *testing.T) {
	for _, in := range []string{"off", "manual", "auto"} {
		if got, err := normalizeMode(in); err != nil || got != in {
			t.Fatalf("normalizeMode(%q) = %q,%v", in, got, err)
		}
	}
	if got, err := normalizeMode("OFF"); err != nil || got != "off" {
		t.Fatalf("normalizeMode(OFF) = %q,%v want off", got, err)
	}
	if _, err := normalizeMode("nope"); err == nil {
		t.Fatal("invalid mode must error")
	}
}

func stubCaffeinateEnv(t *testing.T) *runner.Mock {
	t.Helper()
	prevSupport := caffeinateSupportedFn
	caffeinateSupportedFn = func() bool { return true }
	t.Cleanup(func() { caffeinateSupportedFn = prevSupport })

	prevExec := emeExecutable
	emeExecutable = func() (string, error) { return "/abs/eme", nil }
	t.Cleanup(func() { emeExecutable = prevExec })

	mock := runner.NewMock()
	prevRunner := tmux.Runner
	tmux.Runner = mock
	t.Cleanup(func() { tmux.Runner = prevRunner })

	prevSock := tmux.Socket
	tmux.Socket = ""
	t.Cleanup(func() { tmux.Socket = prevSock })
	return mock
}

func TestArmCaffeinate_SpawnsDaemonWindow(t *testing.T) {
	mock := stubCaffeinateEnv(t)
	sess := &state.Session{ID: "proj-1", TmuxName: "proj", Layout: state.LayoutNestedBare, Root: "/code/proj"}

	// The new-window call must be stubbed: runner.Mock errors on unstubbed calls, and
	// armCaffeinate propagates a NewWindowCmd failure. MainPath() = Root/main for
	// nested-bare. The preceding disarm (kill-window) is unstubbed-but-ignored (best-effort).
	want := []string{"new-window", "-d", "-t", "proj:", "-P", "-F", "#{window_id}",
		"-n", caffeinateWindowName, "-c", "/code/proj/main",
		"/abs/eme", "caffeinate-daemon", "proj-1", "--mode", "auto"}
	mock.Set("tmux", want, "@9", "", nil)

	if err := armCaffeinate(sess, "auto"); err != nil {
		t.Fatalf("armCaffeinate: %v", err)
	}
	// First call disarms any stale window; the new-window call carries the daemon argv.
	var spawned bool
	for _, c := range mock.Calls {
		if len(c.Args) > 0 && c.Args[0] == "new-window" &&
			slices.Contains(c.Args, "caffeinate-daemon") &&
			slices.Contains(c.Args, "proj-1") &&
			slices.Contains(c.Args, "auto") {
			spawned = true
		}
	}
	if !spawned {
		t.Fatalf("expected a new-window with the daemon argv, got %+v", mock.Calls)
	}
}

func TestSetCaffeinate_OffClearsAndDisarms(t *testing.T) {
	mock := stubCaffeinateEnv(t)
	s := &state.State{Version: state.Version, Sessions: []state.Session{
		{ID: "proj-1", TmuxName: "proj", CaffeinateMode: "manual"},
	}}
	withTempStatePath(t, s) // saves under a temp statePath; see helper below
	sess := &s.Sessions[0]

	if err := setCaffeinate(s, sess, "off"); err != nil {
		t.Fatalf("setCaffeinate off: %v", err)
	}
	if sess.CaffeinateMode != "" {
		t.Fatalf("mode = %q, want cleared", sess.CaffeinateMode)
	}
	var killed bool
	for _, c := range mock.Calls {
		if len(c.Args) >= 2 && c.Args[0] == "kill-window" && c.Args[2] == "proj:"+caffeinateWindowName {
			killed = true
		}
	}
	if !killed {
		t.Fatalf("expected kill-window proj:%s, got %+v", caffeinateWindowName, mock.Calls)
	}
}

func withTempStatePath(t *testing.T, s *state.State) {
	t.Helper()
	prev := statePath
	statePath = t.TempDir() + "/state.json"
	t.Cleanup(func() { statePath = prev })
	if s != nil {
		if err := s.Save(statePath); err != nil {
			t.Fatalf("save state: %v", err)
		}
	}
}

func TestSessionStatuses_UsesClassifier(t *testing.T) {
	mock := stubCaffeinateEnv(t)
	// One worktree window @1 with a shell foreground → idle; the daemon's own
	// window is not in state, so it is never counted.
	mock.Set("tmux", []string{"list-panes", "-a", "-F",
		"#{window_id}\t#{pane_dead}\t#{pane_dead_status}\t#{pane_current_command}\t#{window_activity}\t#{@eme_state}\t#{@eme_state_at}"},
		"@1\t0\t0\tzsh\t1782353700\t\t\n", "", nil)
	s := &state.State{Version: state.Version, Sessions: []state.Session{{
		ID: "proj-1", TmuxName: "proj",
		Worktrees: []state.Worktree{{Name: "main", TmuxWindowID: "@1"}},
	}}}
	withTempStatePath(t, s)

	got, ok := sessionStatuses("proj-1")
	if !ok {
		t.Fatal("sessionStatuses: expected ok=true, got false")
	}
	if len(got) != 1 || got[0] != tui.StatusIdle {
		t.Fatalf("sessionStatuses = %v, want [idle]", got)
	}
	if anyWorking(got) {
		t.Fatal("shell foreground must not count as working")
	}
}

func TestSessionStatuses_ReadFailureReturnsNotOk(t *testing.T) {
	// stubCaffeinateEnv sets up the mock runner but we deliberately do NOT stub
	// list-panes, so PanesSnapshot will error → sessionStatuses must return ok=false.
	stubCaffeinateEnv(t)
	s := &state.State{Version: state.Version, Sessions: []state.Session{{
		ID: "proj-1", TmuxName: "proj",
		Worktrees: []state.Worktree{{Name: "main", TmuxWindowID: "@1"}},
	}}}
	withTempStatePath(t, s)

	_, ok := sessionStatuses("proj-1")
	if ok {
		t.Fatal("sessionStatuses: expected ok=false when PanesSnapshot errors, got true")
	}
}

func TestCaffeinateCmd_OffOnNonMac_NoOp(t *testing.T) {
	prev := caffeinateSupportedFn
	caffeinateSupportedFn = func() bool { return false }
	defer func() { caffeinateSupportedFn = prev }()

	caffeinateMode = "manual"
	defer func() { caffeinateMode = "" }()
	// Should return nil without touching tmux/state.
	if err := caffeinateCmd.RunE(caffeinateCmd, []string{"whatever"}); err != nil {
		t.Fatalf("non-mac caffeinate must no-op, got %v", err)
	}
}

func TestReconcileCaffeinate_ReArmsMissingWindow(t *testing.T) {
	mock := stubCaffeinateEnv(t)
	// Session exists; its window list lacks __eme_caffeinate → must re-arm.
	mock.Set("tmux", []string{"has-session", "-t", "proj"}, "", "", nil)
	mock.Set("tmux", []string{"list-windows", "-t", "proj", "-F", "#{window_id}\t#{window_name}"},
		"@1\tmain\n", "", nil)
	s := &state.State{Version: state.Version, Sessions: []state.Session{
		{ID: "proj-1", TmuxName: "proj", CaffeinateMode: "manual"},
	}}

	reconcileCaffeinate(s)

	var rearmed bool
	for _, c := range mock.Calls {
		if len(c.Args) > 0 && c.Args[0] == "new-window" && slices.Contains(c.Args, "caffeinate-daemon") {
			rearmed = true
		}
	}
	if !rearmed {
		t.Fatalf("expected re-arm new-window, got %+v", mock.Calls)
	}
}

func TestReconcileCaffeinate_SkipsWhenPresent(t *testing.T) {
	mock := stubCaffeinateEnv(t)
	mock.Set("tmux", []string{"has-session", "-t", "proj"}, "", "", nil)
	mock.Set("tmux", []string{"list-windows", "-t", "proj", "-F", "#{window_id}\t#{window_name}"},
		"@1\tmain\n@2\t"+caffeinateWindowName+"\n", "", nil)
	s := &state.State{Version: state.Version, Sessions: []state.Session{
		{ID: "proj-1", TmuxName: "proj", CaffeinateMode: "auto"},
	}}

	reconcileCaffeinate(s)

	for _, c := range mock.Calls {
		if len(c.Args) > 0 && c.Args[0] == "new-window" {
			t.Fatalf("must not re-arm when window present, got %+v", mock.Calls)
		}
	}
}
