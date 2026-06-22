package cmd

import (
	"testing"

	"github.com/jinmu/eme/internal/state"
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
