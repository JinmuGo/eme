package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/alderwork/eme/internal/errors"
	"github.com/alderwork/eme/internal/git"
	"github.com/alderwork/eme/internal/runner"
	"github.com/alderwork/eme/internal/state"
)

func TestKillSession_InPlaceNeverDeletesRoot(t *testing.T) {
	sess := &state.Session{
		Root:   "/p/app",
		Layout: state.LayoutInPlace,
		Worktrees: []state.Worktree{
			{Name: "main", Path: "/p/app"},
			{Name: "feat", Path: "/p/app.worktrees/feat"},
		},
	}
	got := pathsToDeleteForKill(sess)
	var rootPresent, siblingPresent bool
	for _, p := range got {
		if p == "/p/app" {
			rootPresent = true
		}
		if p == "/p/app.worktrees/feat" {
			siblingPresent = true
		}
	}
	if rootPresent {
		t.Fatalf("in-place kill must never delete the adopted clone root")
	}
	if !siblingPresent {
		t.Fatalf("in-place kill should delete non-main worktrees; got %v", got)
	}
}

// TestKillSession_PlainDeletesNothing guards plain (non-git) projects: eme created
// nothing on disk for them, so killing must delete no paths. In particular it must
// NOT target <root>/main or <root>/.bare, which could wipe a real subdirectory the
// user happens to have inside their adopted folder.
func TestKillSession_PlainDeletesNothing(t *testing.T) {
	sess := &state.Session{
		Root:      "/p/multirepo",
		Layout:    state.LayoutPlain,
		Worktrees: []state.Worktree{{Name: "main", Path: "/p/multirepo"}},
	}
	if got := pathsToDeleteForKill(sess); len(got) != 0 {
		t.Fatalf("plain kill must delete nothing, got %v", got)
	}
}

// revListArgs is the rev-list invocation UnpushedCommitCount makes for a project's
// .bare, as the mock runner sees it (git.Run prepends -C <gitdir>).
func revListArgs(root string) []string {
	return []string{"-C", root + "/.bare", "rev-list", "--all", "--not", "--remotes", "--count"}
}

// TestUnpushedHistoryGuard locks the safety net: a nested-bare project whose .bare
// holds commits on no remote is refused (its .bare is the only copy of that history),
// while --force-unpushed, a fully-pushed repo, and non-nested-bare layouts pass freely.
func TestUnpushedHistoryGuard(t *testing.T) {
	withCount := func(root, count string) func() {
		mock := runner.NewMock()
		mock.Set("git", revListArgs(root), count, "", nil)
		git.Runner = mock
		return func() { git.Runner = runner.Default }
	}

	t.Run("nested-bare with unpushed history is refused", func(t *testing.T) {
		defer withCount("/p/app", "2\n")()
		sess := &state.Session{DisplayName: "app", Root: "/p/app", Layout: state.LayoutNestedBare}
		err := unpushedHistoryGuard(sess, false)
		if err == nil {
			t.Fatal("expected refusal when history exists only locally")
		}
		if e := errors.As(err); e == nil || e.Code != errors.CodeUnpushedHistory {
			t.Errorf("code = %v, want %s", err, errors.CodeUnpushedHistory)
		}
	})

	t.Run("--force-unpushed bypasses the guard", func(t *testing.T) {
		// forceUnpushed short-circuits before any git call, so no mock is needed.
		git.Runner = runner.NewMock()
		defer func() { git.Runner = runner.Default }()
		sess := &state.Session{DisplayName: "app", Root: "/p/app", Layout: state.LayoutNestedBare}
		if err := unpushedHistoryGuard(sess, true); err != nil {
			t.Errorf("force-unpushed should bypass, got %v", err)
		}
	})

	t.Run("fully pushed nested-bare passes", func(t *testing.T) {
		defer withCount("/p/app", "0\n")()
		sess := &state.Session{DisplayName: "app", Root: "/p/app", Layout: state.LayoutNestedBare}
		if err := unpushedHistoryGuard(sess, false); err != nil {
			t.Errorf("a fully-pushed project must not be blocked, got %v", err)
		}
	})

	t.Run("git error fails open", func(t *testing.T) {
		mock := runner.NewMock()
		mock.Set("git", revListArgs("/p/app"), "", "fatal", fmt.Errorf("exit status 128"))
		git.Runner = mock
		defer func() { git.Runner = runner.Default }()
		sess := &state.Session{DisplayName: "app", Root: "/p/app", Layout: state.LayoutNestedBare}
		if err := unpushedHistoryGuard(sess, false); err != nil {
			t.Errorf("an undeterminable check must not wedge deletion, got %v", err)
		}
	})

	t.Run("non-nested-bare layouts are exempt", func(t *testing.T) {
		// in-place keeps its .git, plain created nothing — neither risks history loss,
		// so the guard returns without consulting git (empty mock would error if called).
		git.Runner = runner.NewMock()
		defer func() { git.Runner = runner.Default }()
		for _, layout := range []string{state.LayoutInPlace, state.LayoutPlain} {
			sess := &state.Session{DisplayName: "app", Root: "/p/app", Layout: layout}
			if err := unpushedHistoryGuard(sess, false); err != nil {
				t.Errorf("layout %q should be exempt, got %v", layout, err)
			}
		}
	})
}

// TestKillSession_RefusesAndPreservesUnpushedNestedBare proves the guard is actually
// WIRED INTO killSession (not just unit-tested in isolation): a refusal must come back
// AND leave the on-disk layout untouched. It fails if the guard call is removed (no
// error, sentinels gone) or moved below the os.RemoveAll loop (sentinels gone).
func TestKillSession_RefusesAndPreservesUnpushedNestedBare(t *testing.T) {
	root := t.TempDir()
	mainDir := filepath.Join(root, "main")
	bareDir := filepath.Join(root, ".bare")
	for _, d := range []string{mainDir, bareDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mock := runner.NewMock()
	mock.Set("git", revListArgs(root), "2\n", "", nil)
	git.Runner = mock
	defer func() { git.Runner = runner.Default }()

	sess := &state.Session{ID: "app", DisplayName: "app", Root: root, Layout: state.LayoutNestedBare}
	err := killSession(&state.State{}, sess, false)
	if e := errors.As(err); e == nil || e.Code != errors.CodeUnpushedHistory {
		t.Fatalf("err = %v, want a CodeUnpushedHistory refusal", err)
	}
	for _, d := range []string{mainDir, bareDir} {
		if _, statErr := os.Stat(d); statErr != nil {
			t.Errorf("%s was deleted despite the refusal: %v", d, statErr)
		}
	}
}

func TestResolveForce(t *testing.T) {
	cases := []struct{ force, unpushed, want bool }{
		{false, false, false},
		{true, false, true},
		{false, true, true}, // --force-unpushed alone implies --force
		{true, true, true},
	}
	for _, c := range cases {
		if got := resolveForce(c.force, c.unpushed); got != c.want {
			t.Errorf("resolveForce(%v, %v) = %v, want %v", c.force, c.unpushed, got, c.want)
		}
	}
}

func TestKillSession_NestedBareDeletesContainer(t *testing.T) {
	sess := &state.Session{Root: "/p/app", Layout: state.LayoutNestedBare}
	got := pathsToDeleteForKill(sess)
	wantMain, wantBare := "/p/app/main", "/p/app/.bare"
	var sawMain, sawBare bool
	for _, p := range got {
		sawMain = sawMain || p == wantMain
		sawBare = sawBare || p == wantBare
	}
	if !sawMain || !sawBare {
		t.Errorf("nested-bare kill should remove main and .bare, got %v", got)
	}
}
