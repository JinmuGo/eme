package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jinmu/eme/internal/git"
	"github.com/jinmu/eme/internal/runner"
	"github.com/jinmu/eme/internal/state"
	"github.com/jinmu/eme/internal/tmux"
)

// windowListFormat mirrors the -F format tmux.ListWindows uses; the mock key must
// match it byte-for-byte (note the literal tab).
const windowListFormat = "#{window_id}\t#{window_name}"

// TestRepairStrandedNestedBare_RebuildsMainWorktree verifies the recovery path for a
// session that reconcile stranded: registered, but with no main worktree and a stale
// in-place layout, while the folder is really nested-bare on disk. The repair must
// restore the main worktree (reusing the live "main" window) and fix the layout.
func TestRepairStrandedNestedBare_RebuildsMainWorktree(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main")
	if err := os.MkdirAll(mainPath, 0o755); err != nil {
		t.Fatal(err)
	}

	mock := runner.NewMock()
	mock.Set("tmux", []string{"-V"}, "tmux 3.4", "", nil)                   // Version
	mock.Set("tmux", []string{"list-sessions"}, "host: 1 windows", "", nil) // ServerReachable
	mock.Set("git", []string{"-C", mainPath, "rev-parse", "--abbrev-ref", "HEAD"}, "main", "", nil)
	mock.Set("tmux", []string{"has-session", "-t", "proj"}, "", "", nil)
	mock.Set("tmux", []string{"list-windows", "-t", "proj", "-F", windowListFormat}, "@7\tmain\n@8\tother", "", nil)

	prevT, prevG := tmux.Runner, git.Runner
	tmux.Runner, git.Runner = mock, mock
	t.Cleanup(func() { tmux.Runner, git.Runner = prevT, prevG })

	prevNS := noSwitchFlag
	noSwitchFlag = true // dashboard path: skip the client switch at the end
	t.Cleanup(func() { noSwitchFlag = prevNS })

	tempState(t) // saveState writes to a throwaway file

	s := &state.State{Version: state.Version, Sessions: []state.Session{{
		ID: "proj-x", DisplayName: "proj", Root: root, TmuxName: "proj",
		AgentCommand: "claude", Layout: state.LayoutInPlace, Worktrees: nil,
	}}}
	sess := s.SessionByRoot(root)

	if err := repairStrandedNestedBare(s, sess, root); err != nil {
		t.Fatalf("repairStrandedNestedBare: %v", err)
	}
	if sess.Layout != state.LayoutNestedBare {
		t.Errorf("layout = %q, want %q", sess.Layout, state.LayoutNestedBare)
	}
	w := sess.WorktreeByName("main")
	if w == nil {
		t.Fatal("main worktree was not restored")
	}
	if w.Path != mainPath {
		t.Errorf("worktree path = %q, want %q", w.Path, mainPath)
	}
	if w.TmuxWindowID != "@7" {
		t.Errorf("window id = %q, want @7 (the live 'main' window reused)", w.TmuxWindowID)
	}
	if w.Branch != "main" {
		t.Errorf("branch = %q, want main", w.Branch)
	}
}

// TestRepairStrandedNestedBare_MissingMainOnDiskErrors guards the case where the
// state is stranded AND the folder has no main worktree to recover from: the repair
// must refuse with a clear error rather than register a bogus worktree.
func TestRepairStrandedNestedBare_MissingMainOnDiskErrors(t *testing.T) {
	root := t.TempDir() // no <root>/main

	mock := runner.NewMock()
	mock.Set("tmux", []string{"-V"}, "tmux 3.4", "", nil)
	mock.Set("tmux", []string{"list-sessions"}, "host: 1 windows", "", nil)
	prevT := tmux.Runner
	tmux.Runner = mock
	t.Cleanup(func() { tmux.Runner = prevT })

	s := &state.State{Version: state.Version, Sessions: []state.Session{{
		ID: "proj-x", DisplayName: "proj", Root: root, TmuxName: "proj", Layout: state.LayoutInPlace,
	}}}
	sess := s.SessionByRoot(root)

	if err := repairStrandedNestedBare(s, sess, root); err == nil {
		t.Fatal("expected an error when no main worktree exists on disk, got nil")
	}
}

// TestEnsureMainWindow_ReusesExistingMainWindow checks that a live window named
// "main" is reused (so the user's pane is preserved) rather than a new one created.
func TestEnsureMainWindow_ReusesExistingMainWindow(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("tmux", []string{"has-session", "-t", "proj"}, "", "", nil)
	mock.Set("tmux", []string{"list-windows", "-t", "proj", "-F", windowListFormat}, "@7\tmain\n@8\tother", "", nil)
	prev := tmux.Runner
	tmux.Runner = mock
	t.Cleanup(func() { tmux.Runner = prev })

	id, err := ensureMainWindow("proj", "/x/proj/main")
	if err != nil {
		t.Fatal(err)
	}
	if id != "@7" {
		t.Errorf("ensureMainWindow = %q, want @7 (reused)", id)
	}
}

// TestEnsureMainWindow_CreatesWhenNoMainWindow checks that a new "main" window is
// created when the live session has none.
func TestEnsureMainWindow_CreatesWhenNoMainWindow(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("tmux", []string{"has-session", "-t", "proj"}, "", "", nil)
	mock.Set("tmux", []string{"list-windows", "-t", "proj", "-F", windowListFormat}, "@8\tother", "", nil)
	mock.Set("tmux", []string{"new-window", "-t", "proj:", "-P", "-F", "#{window_id}", "-n", "main", "-c", "/x/proj/main"}, "@9", "", nil)
	prev := tmux.Runner
	tmux.Runner = mock
	t.Cleanup(func() { tmux.Runner = prev })

	id, err := ensureMainWindow("proj", "/x/proj/main")
	if err != nil {
		t.Fatal(err)
	}
	if id != "@9" {
		t.Errorf("ensureMainWindow = %q, want @9 (created)", id)
	}
}
