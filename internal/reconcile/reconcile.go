// Package reconcile merges cached eme state with live tmux and git state.
package reconcile

import (
	"os"
	"path/filepath"

	"github.com/jinmu/eme/internal/git"
	"github.com/jinmu/eme/internal/state"
	"github.com/jinmu/eme/internal/tmux"
)

// State reconciles cached state with live tmux and git state.
// It returns true if the state was modified.
func State(s *state.State) bool {
	modified := false

	liveSessions, err := tmux.ListSessions()
	if err != nil {
		liveSessions = map[string]string{}
	}

	keptSessions := s.Sessions[:0]
	for _, sess := range s.Sessions {
		if _, ok := liveSessions[sess.TmuxName]; !ok {
			modified = true
			continue
		}

		keptWorktrees := sess.Worktrees[:0]
		for _, w := range sess.Worktrees {
			if !worktreeExists(sess, w) {
				modified = true
				continue
			}
			keptWorktrees = append(keptWorktrees, w)
		}
		sess.Worktrees = keptWorktrees
		keptSessions = append(keptSessions, sess)
	}
	s.Sessions = keptSessions

	return modified
}

func worktreeExists(sess state.Session, w state.Worktree) bool {
	if _, err := os.Stat(w.Path); err != nil {
		return false
	}
	// Check tmux window exists.
	windows, err := tmux.ListWindows(sess.TmuxName)
	if err != nil {
		return false
	}
	if _, ok := windows[w.TmuxWindowID]; !ok {
		return false
	}
	// Check git worktree exists.
	if _, err := git.TopLevel(w.Path); err != nil {
		return false
	}
	// Check git worktree is not prunable.
	entries, err := git.WorktreeListPorcelain(sess.MainPath())
	if err == nil {
		lookup := w.Path
		if resolved, rerr := filepath.EvalSymlinks(w.Path); rerr == nil {
			lookup = resolved
		}
		if prunablePaths(entries)[lookup] {
			return false
		}
	}
	return true
}

// prunablePaths returns the set of worktree paths git reports as prunable,
// keyed by their symlink-resolved (canonical) form so lookups match git's
// canonical porcelain output regardless of how the path was stored in state.
func prunablePaths(entries []git.WorktreeEntry) map[string]bool {
	out := make(map[string]bool)
	for _, e := range entries {
		if !e.Prunable {
			continue
		}
		p := e.Path
		if resolved, err := filepath.EvalSymlinks(e.Path); err == nil {
			p = resolved
		}
		out[p] = true
	}
	return out
}
