// Package reconcile merges cached eme state with live tmux and git state.
package reconcile

import (
	"os"

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
	return true
}
