package cmd

import (
	"path/filepath"
	"strings"

	"github.com/jinmu/eme/internal/git"
	"github.com/jinmu/eme/internal/state"
	"github.com/jinmu/eme/internal/tui"
)

// buildSessionViews maps reconciled state into render-ready dashboard views,
// deriving each worktree's agent status, optional diff stat, and a short agent
// label. Process/git inspection lives here so the tui package stays pure.
func buildSessionViews(sessions []state.Session) []tui.SessionView {
	views := make([]tui.SessionView, 0, len(sessions))
	for i := range sessions {
		s := &sessions[i]
		sv := tui.SessionView{DisplayName: s.DisplayName, Root: s.Root}
		for j := range s.Worktrees {
			w := &s.Worktrees[j]
			status := agentStatus(w)
			wv := tui.WorktreeView{
				Name:      w.Name,
				Branch:    w.Branch,
				SessionID: s.ID,
				IsMain:    w.Name == "main",
				Status:    status,
			}
			if status == tui.StatusWorking {
				wv.AgentLabel = agentLabel(w)
			}
			if added, deleted, ok := git.DiffStat(w.Path); ok {
				wv.Added, wv.Deleted, wv.HasDiff = added, deleted, true
			}
			sv.Worktrees = append(sv.Worktrees, wv)
		}
		views = append(views, sv)
	}
	return views
}

// agentStatus derives a worktree's agent lifecycle state. reconcile does not
// clear AgentPID, so a recorded PID may be stale: a live PID means working,
// otherwise a previously recorded agent means it exited.
func agentStatus(w *state.Worktree) tui.AgentStatus {
	if w.AgentPID > 0 && processExists(w.AgentPID) {
		return tui.StatusWorking
	}
	if w.AgentPID > 0 || w.LastAgentCommand != "" {
		return tui.StatusExited
	}
	return tui.StatusIdle
}

// agentLabel returns the agent binary's basename from the command that started
// it, for display next to a working agent.
func agentLabel(w *state.Worktree) string {
	fields := strings.Fields(w.LastAgentCommand)
	if len(fields) == 0 {
		return ""
	}
	return filepath.Base(fields[0])
}
