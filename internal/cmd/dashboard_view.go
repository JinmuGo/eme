package cmd

import (
	"path/filepath"
	"strings"

	"github.com/jinmu/eme/internal/git"
	"github.com/jinmu/eme/internal/state"
	"github.com/jinmu/eme/internal/tmux"
	"github.com/jinmu/eme/internal/tui"
)

// buildSessionViews maps reconciled state into render-ready dashboard views,
// deriving each worktree's agent status from a tmux pane snapshot (taken once by
// the caller), plus an optional diff stat and a short agent label. The snapshot is
// injected so this stays pure and testable; status/git inspection lives here so the
// tui package stays presentation-only.
func buildSessionViews(sessions []state.Session, snap map[string]tmux.PaneInfo) []tui.SessionView {
	views := make([]tui.SessionView, 0, len(sessions))
	for i := range sessions {
		s := &sessions[i]
		sv := tui.SessionView{DisplayName: s.DisplayName, Root: s.Root}
		for j := range s.Worktrees {
			w := &s.Worktrees[j]
			info, present := snap[w.TmuxWindowID]
			status := classifyStatus(info, present, w.LastAgentCommand)
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

// classifyStatus derives a worktree's agent lifecycle from its pane snapshot. It
// keys off pane_dead (structural), NOT pane_current_command == agent name, because
// an interactive agent runs under a different process name (claude runs as `node`,
// verified by the T0 experiment). Under the exec launch model the agent IS the pane
// process, so its exit status surfaces directly:
//
//	window gone, agent ran → exited
//	pane dead, status 0    → exited (clean)  ○
//	pane dead, status != 0 → crashed         ✗
//	pane alive, agent ran  → running         ◐  (working|waiting lumped, DESIGN.md §5.2)
//	pane alive, no agent   → idle            ·
//
// present is false when the worktree's window has no pane in the snapshot.
func classifyStatus(info tmux.PaneInfo, present bool, lastAgentCmd string) tui.AgentStatus {
	if !present {
		if lastAgentCmd != "" {
			return tui.StatusExited
		}
		return tui.StatusIdle
	}
	if info.Dead {
		if info.DeadStatus == 0 {
			return tui.StatusExited
		}
		return tui.StatusCrashed
	}
	if lastAgentCmd == "" {
		return tui.StatusIdle // a live shell that never ran an agent
	}
	return tui.StatusWorking
}

// agentLabel returns the agent binary's basename from the command that started it,
// for display next to a running agent.
func agentLabel(w *state.Worktree) string {
	fields := strings.Fields(w.LastAgentCommand)
	if len(fields) == 0 {
		return ""
	}
	return filepath.Base(fields[0])
}
