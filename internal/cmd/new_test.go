package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinmu/eme/internal/errors"
	"github.com/jinmu/eme/internal/git"
	"github.com/jinmu/eme/internal/runner"
	"github.com/jinmu/eme/internal/state"
	"github.com/jinmu/eme/internal/tmux"
)

func TestDirIsEffectivelyEmpty(t *testing.T) {
	t.Run("empty dir is empty", func(t *testing.T) {
		dir := t.TempDir()
		if ok, err := dirIsEffectivelyEmpty(dir); err != nil || !ok {
			t.Fatalf("empty dir: got (%v, %v), want (true, nil)", ok, err)
		}
	})
	t.Run("only .DS_Store is still empty", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if ok, err := dirIsEffectivelyEmpty(dir); err != nil || !ok {
			t.Fatalf("only .DS_Store: got (%v, %v), want (true, nil)", ok, err)
		}
	})
	t.Run("a real file makes it non-empty", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if ok, err := dirIsEffectivelyEmpty(dir); err != nil || ok {
			t.Fatalf("with README: got (%v, %v), want (false, nil)", ok, err)
		}
	})
	t.Run("a subdirectory makes it non-empty", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
		if ok, err := dirIsEffectivelyEmpty(dir); err != nil || ok {
			t.Fatalf("with subdir: got (%v, %v), want (false, nil)", ok, err)
		}
	})
}

func TestRouteByClassification(t *testing.T) {
	cases := []struct {
		kind    git.Kind
		wantErr string // expected error code
	}{
		{git.KindSubmodule, errors.CodeSubmoduleRepo},
		{git.KindBareRepo, errors.CodeBareRepo},
		{git.KindBrokenGit, errors.CodeBrokenGit},
	}
	for _, tc := range cases {
		err := routeByClassification(git.Classification{Kind: tc.kind, TopLevel: "/x"}, false)
		if e := errors.As(err); e == nil || e.Code != tc.wantErr {
			t.Errorf("kind %v: got %v, want code %s", tc.kind, err, tc.wantErr)
		}
	}
}

func TestCreateProject_EmptyFolderRejected(t *testing.T) {
	// Regression: a cancelled folder picker used to reach createProject with an
	// empty path. filepath.Abs("") resolves to the cwd, so createProject would
	// classify the current directory and adopt/switch to it — the "session jump
	// on Ctrl+C" bug. The guard must reject empty/whitespace before any side
	// effect (no git, tmux, or state access).
	for _, in := range []string{"", "   ", "\t"} {
		err := createProject(in)
		e := errors.As(err)
		if e == nil || e.Code != errors.CodeInvalidFolder {
			t.Errorf("createProject(%q) = %v, want code %s", in, err, errors.CodeInvalidFolder)
		}
	}
}

// TestCreateWorktree_RefusesOnPlainLayout verifies a plain (non-git) project
// rejects worktree creation with a clear error instead of shelling out to git
// (which would fail with a raw "not a git repository").
func TestCreateWorktree_RefusesOnPlainLayout(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("tmux", []string{"-V"}, "tmux 3.4", "", nil)
	mock.Set("tmux", []string{"list-sessions"}, "host: 1 windows", "", nil)
	prev := tmux.Runner
	tmux.Runner = mock
	t.Cleanup(func() { tmux.Runner = prev })

	tempState(t)
	s := &state.State{Version: state.Version, Sessions: []state.Session{{
		ID: "plain-x", DisplayName: "cinchcli", Root: "/x/cinchcli", TmuxName: "cinchcli",
		Layout:    state.LayoutPlain,
		Worktrees: []state.Worktree{{Name: "main", Path: "/x/cinchcli", TmuxWindowID: "@1"}},
	}}}
	if err := saveState(s); err != nil {
		t.Fatal(err)
	}

	err := createWorktree("cinchcli", "feature")
	if e := errors.As(err); e == nil || e.Code != errors.CodeInvalidFolder {
		t.Fatalf("createWorktree on plain layout = %v, want code %s", err, errors.CodeInvalidFolder)
	}
}

// TestCreateWorktree_RefusesDFNamespaceConflict verifies eme catches a directory/file
// branch-ref conflict (e.g. "feat" when "feat/x" exists) with an actionable error
// before shelling out to git — the real-world cause of the raw "exit status 255".
func TestCreateWorktree_RefusesDFNamespaceConflict(t *testing.T) {
	tmock := runner.NewMock()
	tmock.Set("tmux", []string{"-V"}, "tmux 3.4", "", nil)
	tmock.Set("tmux", []string{"list-sessions"}, "host: 1 windows", "", nil)
	gmock := runner.NewMock()
	// Not checked out anywhere, and "feat" is NOT an exact branch nor a remote...
	gmock.Set("git", []string{"-C", "/repo", "worktree", "list", "--porcelain"},
		"worktree /repo\nHEAD a1\nbranch refs/heads/main\n", "", nil)
	gmock.Set("git", []string{"-C", "/repo", "show-ref", "--verify", "--quiet", "refs/heads/feat"},
		"", "", fmt.Errorf("exit status 1"))
	gmock.Set("git", []string{"-C", "/repo", "for-each-ref", "--format=%(refname)", "refs/remotes/"}, "", "", nil)
	// ...but refs/heads/feat/ is an occupied namespace → D/F conflict, with candidates.
	gmock.Set("git", []string{"-C", "/repo", "for-each-ref", "--format=%(refname:short)", "refs/heads/"},
		"feat/design-polish\nmain", "", nil)
	gmock.Set("git", []string{"-C", "/repo", "config", "--get", "core.ignorecase"}, "false", "", nil)
	prevT, prevG := tmux.Runner, git.Runner
	tmux.Runner, git.Runner = tmock, gmock
	t.Cleanup(func() { tmux.Runner, git.Runner = prevT, prevG })

	tempState(t)
	s := &state.State{Version: state.Version, Sessions: []state.Session{{
		ID: "repo-x", DisplayName: "repo", Root: "/repo", TmuxName: "repo",
		Layout:    state.LayoutInPlace,
		Worktrees: []state.Worktree{{Name: "main", Path: "/repo", TmuxWindowID: "@1"}},
	}}}
	if err := saveState(s); err != nil {
		t.Fatal(err)
	}

	err := createWorktree("repo", "feat")
	e := errors.As(err)
	if e == nil || e.Code != errors.CodeBranchExists {
		t.Fatalf("createWorktree feat (with feat/* present) = %v, want code %s", err, errors.CodeBranchExists)
	}
	if !strings.Contains(e.Fix, "feat/design-polish") {
		t.Errorf("D/F error should list the existing branch as a candidate, got Fix=%q", e.Fix)
	}
}

// TestCreateWorktree_SwitchesWhenBranchAlreadyCheckedOut verifies the DWIM helper:
// asking for a worktree on a branch that is already live in an eme worktree switches
// to it instead of erroring or creating a duplicate.
func TestCreateWorktree_SwitchesWhenBranchAlreadyCheckedOut(t *testing.T) {
	prevNS := noSwitchFlag
	noSwitchFlag = true // make maybeSwitchClient a no-op; we assert the decision, not tmux
	t.Cleanup(func() { noSwitchFlag = prevNS })

	tmock := runner.NewMock()
	tmock.Set("tmux", []string{"-V"}, "tmux 3.4", "", nil)
	tmock.Set("tmux", []string{"list-sessions"}, "host: 1 windows", "", nil)
	gmock := runner.NewMock()
	// "feat-1" is checked out in the eme worktree at /repo.worktrees/feat-1.
	gmock.Set("git", []string{"-C", "/repo", "worktree", "list", "--porcelain"},
		"worktree /repo\nHEAD a1\nbranch refs/heads/main\n\nworktree /repo.worktrees/feat-1\nHEAD b2\nbranch refs/heads/feat-1\n", "", nil)
	prevT, prevG := tmux.Runner, git.Runner
	tmux.Runner, git.Runner = tmock, gmock
	t.Cleanup(func() { tmux.Runner, git.Runner = prevT, prevG })

	tempState(t)
	s := &state.State{Version: state.Version, Sessions: []state.Session{{
		ID: "repo-x", DisplayName: "repo", Root: "/repo", TmuxName: "repo", Layout: state.LayoutInPlace,
		Worktrees: []state.Worktree{
			{Name: "main", Path: "/repo", TmuxWindowID: "@1"},
			{Name: "feat-1", Path: "/repo.worktrees/feat-1", Branch: "feat-1", TmuxWindowID: "@2"},
		},
	}}}
	if err := saveState(s); err != nil {
		t.Fatal(err)
	}

	// Should switch (return nil) without attempting any worktree add.
	if err := createWorktree("repo", "feat-1"); err != nil {
		t.Fatalf("createWorktree on an already-checked-out branch should switch, got %v", err)
	}
	for _, c := range gmock.Calls {
		if len(c.Args) >= 4 && c.Args[2] == "worktree" && c.Args[3] == "add" {
			t.Errorf("must not run `worktree add` when switching to an existing checkout: %v", c.Args)
		}
	}
}

// TestCreateWorktree_ChecksOutExistingBranch verifies the core DWIM behavior: a name
// matching an existing branch checks it OUT (git worktree add <path> <branch>, no -b)
// instead of refusing.
func TestCreateWorktree_ChecksOutExistingBranch(t *testing.T) {
	prevNS := noSwitchFlag
	noSwitchFlag = true
	t.Cleanup(func() { noSwitchFlag = prevNS })

	repo := t.TempDir()
	name := "existing-feat"
	target := filepath.Join(repo+".worktrees", name)
	t.Cleanup(func() { os.RemoveAll(repo + ".worktrees") })

	tmock := runner.NewMock()
	tmock.Set("tmux", []string{"-V"}, "tmux 3.4", "", nil)
	tmock.Set("tmux", []string{"list-sessions"}, "host: 1 windows", "", nil)
	tmock.Set("tmux", []string{"new-window", "-t", "repo:", "-P", "-F", "#{window_id}", "-n", name, "-c", target}, "@9", "", nil)

	gmock := runner.NewMock()
	gmock.Set("git", []string{"-C", repo, "worktree", "list", "--porcelain"},
		"worktree "+repo+"\nHEAD a1\nbranch refs/heads/main\n", "", nil) // not checked out
	gmock.Set("git", []string{"-C", repo, "show-ref", "--verify", "--quiet", "refs/heads/" + name}, "", "", nil) // branch exists
	gmock.Set("git", []string{"-C", repo, "for-each-ref", "--format=%(refname)", "refs/remotes/"}, "", "", nil)  // no remotes
	// checkout form: `worktree add <target> <name>` (NO -b).
	gmock.Set("git", []string{"-C", repo, "worktree", "add", target, name}, "", "", nil)
	gmock.Set("git", []string{"-C", target, "rev-parse", "--abbrev-ref", "HEAD"}, name, "", nil)

	prevT, prevG := tmux.Runner, git.Runner
	tmux.Runner, git.Runner = tmock, gmock
	t.Cleanup(func() { tmux.Runner, git.Runner = prevT, prevG })

	tempState(t)
	s := &state.State{Version: state.Version, Sessions: []state.Session{{
		ID: "repo-x", DisplayName: "repo", Root: repo, TmuxName: "repo", Layout: state.LayoutInPlace,
		Worktrees: []state.Worktree{{Name: "main", Path: repo, TmuxWindowID: "@1"}},
	}}}
	if err := saveState(s); err != nil {
		t.Fatal(err)
	}

	if err := createWorktree("repo", name); err != nil {
		t.Fatalf("checkout of an existing branch should succeed, got %v", err)
	}
	// Assert the checkout form was used (worktree add <target> <name>, never -b).
	var sawCheckout bool
	for _, c := range gmock.Calls {
		if len(c.Args) >= 4 && c.Args[2] == "worktree" && c.Args[3] == "add" {
			for _, a := range c.Args {
				if a == "-b" {
					t.Errorf("existing branch must be checked out, not recreated with -b: %v", c.Args)
				}
			}
			if c.Args[len(c.Args)-1] == name && c.Args[len(c.Args)-2] == target {
				sawCheckout = true
			}
		}
	}
	if !sawCheckout {
		t.Errorf("expected `git worktree add %s %s`, calls=%v", target, name, gmock.Calls)
	}
	// And the worktree is now registered.
	reloaded, _ := loadState()
	if reloaded.Sessions[0].WorktreeByName(name) == nil {
		t.Errorf("checked-out worktree %q was not registered", name)
	}
}

// TestCreateWorktree_RefusesAmbiguousMultiRemote guards the regression the review
// found: a branch carried by two remotes (no local) must not be fed to `git worktree
// add <name>` (git refuses to guess) — eme refuses early with a clear, named error.
func TestCreateWorktree_RefusesAmbiguousMultiRemote(t *testing.T) {
	tmock := runner.NewMock()
	tmock.Set("tmux", []string{"-V"}, "tmux 3.4", "", nil)
	tmock.Set("tmux", []string{"list-sessions"}, "host: 1 windows", "", nil)
	gmock := runner.NewMock()
	gmock.Set("git", []string{"-C", "/repo", "worktree", "list", "--porcelain"},
		"worktree /repo\nHEAD a1\nbranch refs/heads/main\n", "", nil)
	gmock.Set("git", []string{"-C", "/repo", "show-ref", "--verify", "--quiet", "refs/heads/shared"},
		"", "", fmt.Errorf("exit 1")) // no local branch
	gmock.Set("git", []string{"-C", "/repo", "for-each-ref", "--format=%(refname)", "refs/remotes/"},
		"refs/remotes/origin/shared\nrefs/remotes/upstream/shared", "", nil) // two remotes
	prevT, prevG := tmux.Runner, git.Runner
	tmux.Runner, git.Runner = tmock, gmock
	t.Cleanup(func() { tmux.Runner, git.Runner = prevT, prevG })

	tempState(t)
	s := &state.State{Version: state.Version, Sessions: []state.Session{{
		ID: "repo-x", DisplayName: "repo", Root: "/repo", TmuxName: "repo", Layout: state.LayoutInPlace,
		Worktrees: []state.Worktree{{Name: "main", Path: "/repo", TmuxWindowID: "@1"}},
	}}}
	if err := saveState(s); err != nil {
		t.Fatal(err)
	}
	err := createWorktree("repo", "shared")
	e := errors.As(err)
	if e == nil || e.Code != errors.CodeBranchExists {
		t.Fatalf("ambiguous multi-remote = %v, want code %s", err, errors.CodeBranchExists)
	}
	if !strings.Contains(e.Message, "multiple remotes") {
		t.Errorf("error should name the ambiguity, got %q", e.Message)
	}
	for _, c := range gmock.Calls {
		if len(c.Args) >= 4 && c.Args[2] == "worktree" && c.Args[3] == "add" {
			t.Errorf("must not run worktree add on an ambiguous remote branch: %v", c.Args)
		}
	}
}

// TestCreateWorktree_SwitchesToMainWhenBranchLivesThere exercises the path-based switch:
// the branch is checked out in the main worktree (whose eme name "main" != the branch),
// so the name lookup misses but BranchCheckedOutAt + worktreeAtPath route to it.
func TestCreateWorktree_SwitchesToMainWhenBranchLivesThere(t *testing.T) {
	prevNS := noSwitchFlag
	noSwitchFlag = true
	t.Cleanup(func() { noSwitchFlag = prevNS })

	tmock := runner.NewMock()
	tmock.Set("tmux", []string{"-V"}, "tmux 3.4", "", nil)
	tmock.Set("tmux", []string{"list-sessions"}, "host: 1 windows", "", nil)
	gmock := runner.NewMock()
	gmock.Set("git", []string{"-C", "/repo", "worktree", "list", "--porcelain"},
		"worktree /repo\nHEAD a1\nbranch refs/heads/feature-x\n", "", nil)
	prevT, prevG := tmux.Runner, git.Runner
	tmux.Runner, git.Runner = tmock, gmock
	t.Cleanup(func() { tmux.Runner, git.Runner = prevT, prevG })

	tempState(t)
	s := &state.State{Version: state.Version, Sessions: []state.Session{{
		ID: "repo-x", DisplayName: "repo", Root: "/repo", TmuxName: "repo", Layout: state.LayoutInPlace,
		Worktrees: []state.Worktree{{Name: "main", Path: "/repo", Branch: "feature-x", TmuxWindowID: "@1"}},
	}}}
	if err := saveState(s); err != nil {
		t.Fatal(err)
	}
	if err := createWorktree("repo", "feature-x"); err != nil {
		t.Fatalf("should switch to main where the branch lives, got %v", err)
	}
	for _, c := range gmock.Calls {
		if len(c.Args) >= 4 && c.Args[2] == "worktree" && c.Args[3] == "add" {
			t.Errorf("must not create a worktree when the branch is already on main: %v", c.Args)
		}
	}
}

// TestCreateWorktree_CleanupDeletesNewBranchOnWindowFailure guards the orphan-branch
// fix: when a NEW branch was created and the tmux window then fails, cleanup must delete
// that branch so a retry doesn't silently check out the half-built leftover.
func TestCreateWorktree_CleanupDeletesNewBranchOnWindowFailure(t *testing.T) {
	prevNS := noSwitchFlag
	noSwitchFlag = true
	t.Cleanup(func() { noSwitchFlag = prevNS })

	repo := t.TempDir()
	name := "brand-new"
	target := filepath.Join(repo+".worktrees", name)
	t.Cleanup(func() { os.RemoveAll(repo + ".worktrees") })

	tmock := runner.NewMock()
	tmock.Set("tmux", []string{"-V"}, "tmux 3.4", "", nil)
	tmock.Set("tmux", []string{"list-sessions"}, "host: 1 windows", "", nil)
	tmock.Set("tmux", []string{"new-window", "-t", "repo:", "-P", "-F", "#{window_id}", "-n", name, "-c", target},
		"", "boom", fmt.Errorf("exit 1")) // window creation FAILS

	gmock := runner.NewMock()
	gmock.Set("git", []string{"-C", repo, "worktree", "list", "--porcelain"}, "worktree "+repo+"\nHEAD a1\nbranch refs/heads/main\n", "", nil)
	gmock.Set("git", []string{"-C", repo, "show-ref", "--verify", "--quiet", "refs/heads/" + name}, "", "", fmt.Errorf("exit 1")) // no local branch
	gmock.Set("git", []string{"-C", repo, "for-each-ref", "--format=%(refname)", "refs/remotes/"}, "", "", nil)                   // no remotes
	gmock.Set("git", []string{"-C", repo, "for-each-ref", "--format=%(refname:short)", "refs/heads/"}, "main", "", nil)           // D/F: none
	gmock.Set("git", []string{"-C", repo, "config", "--get", "core.ignorecase"}, "false", "", nil)
	gmock.Set("git", []string{"-C", repo, "worktree", "add", "-b", name, target}, "", "", nil) // new branch created
	prevT, prevG := tmux.Runner, git.Runner
	tmux.Runner, git.Runner = tmock, gmock
	t.Cleanup(func() { tmux.Runner, git.Runner = prevT, prevG })

	tempState(t)
	s := &state.State{Version: state.Version, Sessions: []state.Session{{
		ID: "repo-x", DisplayName: "repo", Root: repo, TmuxName: "repo", Layout: state.LayoutInPlace,
		Worktrees: []state.Worktree{{Name: "main", Path: repo, TmuxWindowID: "@1"}},
	}}}
	if err := saveState(s); err != nil {
		t.Fatal(err)
	}
	if err := createWorktree("repo", name); err == nil {
		t.Fatal("expected an error when the tmux window fails")
	}
	var deleted bool
	for _, c := range gmock.Calls {
		if len(c.Args) >= 5 && c.Args[2] == "branch" && c.Args[3] == "-D" && c.Args[4] == name {
			deleted = true
		}
	}
	if !deleted {
		t.Errorf("cleanup must delete the branch eme created; calls=%v", gmock.Calls)
	}
}

func TestWorktreeTargetPath(t *testing.T) {
	nested := &state.Session{Root: "/p/app", Layout: state.LayoutNestedBare}
	if got := worktreeTargetPath(nested, "feat"); got != "/p/app/feat" {
		t.Errorf("nested target = %q", got)
	}
	inplace := &state.Session{Root: "/p/app", Layout: state.LayoutInPlace}
	if got := worktreeTargetPath(inplace, "feat"); got != "/p/app.worktrees/feat" {
		t.Errorf("in-place target = %q", got)
	}
}

func TestConvertFlagRegistered(t *testing.T) {
	if newCmd.Flags().Lookup("convert") == nil {
		t.Errorf("--convert flag not registered on newCmd")
	}
}

func TestNoSwitchFlagRegistered(t *testing.T) {
	if newCmd.Flags().Lookup("no-switch") == nil {
		t.Errorf("--no-switch flag not registered on newCmd")
	}
}

func TestSwitchToSession_NoSwitchIsNoop(t *testing.T) {
	// With --no-switch (the dashboard's create path) switchToSession must return
	// nil before touching tmux or the session's worktrees, so the dashboard
	// stays put instead of jumping to an already-managed project.
	prev := noSwitchFlag
	noSwitchFlag = true
	defer func() { noSwitchFlag = prev }()

	if err := switchToSession(&state.Session{DisplayName: "x"}); err != nil {
		t.Errorf("switchToSession with --no-switch = %v, want nil", err)
	}
}

func TestScanFolders_DeduplicatesAndSkipsHidden(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create a visible dir and a hidden dir.
	visible := filepath.Join(home, "Projects")
	if err := os.MkdirAll(visible, 0o755); err != nil {
		t.Fatal(err)
	}
	hidden := filepath.Join(home, ".hidden")
	if err := os.MkdirAll(hidden, 0o755); err != nil {
		t.Fatal(err)
	}

	folders, err := scanFolders()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	seen := make(map[string]bool)
	for _, f := range folders {
		if seen[f] {
			t.Errorf("duplicate folder %q", f)
		}
		seen[f] = true
		if filepath.Base(f) == ".hidden" {
			t.Errorf("hidden folder should be skipped: %q", f)
		}
	}
}
