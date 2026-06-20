package reconcile

import (
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
