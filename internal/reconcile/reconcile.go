// Package reconcile merges cached eme state with live tmux and git state.
package reconcile

import (
	"os"
	"path/filepath"

	"github.com/alderwork/eme/internal/git"
	"github.com/alderwork/eme/internal/state"
	"github.com/alderwork/eme/internal/tmux"
)

// State reconciles cached state with live tmux and git state.
// It returns true if the state was modified.
func State(s *state.State) bool {
	liveSessions, err := tmux.ListSessions()
	if err != nil {
		// The tmux server is unreachable (not running yet, or eme is pinned to a
		// socket whose server is down). Treating "can't see it" as "it's gone"
		// would prune every session and the caller would persist that empty
		// state, destroying records for sessions that are merely unreachable
		// right now. Leave state untouched until we can compare against a live
		// server. A running tmux server always reports at least one session, so
		// an error here reliably means "no server", not "zero sessions".
		return false
	}

	modified := false
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
	// A plain (non-git) project has no git worktree to validate or prune; the
	// directory + tmux window existing is the whole liveness check. Running the
	// git checks below would error (not a repo) and wrongly prune it away.
	if sess.Layout == state.LayoutPlain {
		return true
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
