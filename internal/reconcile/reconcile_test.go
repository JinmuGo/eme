package reconcile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jinmu/eme/internal/git"
)

func TestPrunablePaths(t *testing.T) {
	entries := []git.WorktreeEntry{
		{Path: "/repo", Branch: "main"},
		{Path: "/repo.worktrees/dead", Prunable: true},
		{Path: "/repo.worktrees/live", Branch: "live"},
	}
	got := prunablePaths(entries)
	if !got["/repo.worktrees/dead"] {
		t.Errorf("dead should be prunable")
	}
	if got["/repo.worktrees/live"] || got["/repo"] {
		t.Errorf("live/main must not be prunable: %v", got)
	}
}

func TestPrunablePaths_ResolvesSymlinks(t *testing.T) {
	base := t.TempDir()
	realDir := filepath.Join(base, "real")
	linkDir := filepath.Join(base, "link")

	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("failed to create real dir: %v", err)
	}

	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	// Entry uses the symlink path
	entries := []git.WorktreeEntry{{Path: linkDir, Prunable: true}}

	// prunablePaths should resolve the symlink and key by canonical form
	got := prunablePaths(entries)

	// Verify the map is keyed by the resolved real path
	want, _ := filepath.EvalSymlinks(realDir)
	if !got[want] {
		t.Errorf("prunablePaths should contain resolved symlink path %q: %v", want, got)
	}
}
