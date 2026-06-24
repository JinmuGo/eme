package cmd

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jinmu/eme/internal/git"
	"github.com/jinmu/eme/internal/state"
	"github.com/jinmu/eme/internal/tmux"
	"github.com/jinmu/eme/internal/tui"
)

// buildSessionViews maps state into render-ready dashboard views with the FULL
// inspection: agent status from the injected pane snapshot plus a per-worktree git
// diff stat. Used on the initial load and after each child action. The snapshot is
// injected so this stays pure and testable; status/git inspection lives here so the
// tui package stays presentation-only.
func buildSessionViews(sessions []state.Session, snap map[string]tmux.PaneInfo, now time.Time, quietAfter time.Duration) []tui.SessionView {
	return buildViews(sessions, snap, true, now, quietAfter)
}

// buildStatusViews is the cheap status-only path for the auto-refresh ticker (and,
// later, the `eme status` segment): it derives agent status from the snapshot but
// skips the per-worktree git diff — a subprocess per worktree that, at the tick
// cadence across many worktrees, is real churn. The dashboard recomputes diffs only
// on a full reload and carries the last-known stats forward between ticks.
func buildStatusViews(sessions []state.Session, snap map[string]tmux.PaneInfo, now time.Time, quietAfter time.Duration) []tui.SessionView {
	return buildViews(sessions, snap, false, now, quietAfter)
}

// buildViews is the shared mapper; withDiff toggles the expensive git.DiffStat call.
func buildViews(sessions []state.Session, snap map[string]tmux.PaneInfo, withDiff bool, now time.Time, quietAfter time.Duration) []tui.SessionView {
	views := make([]tui.SessionView, 0, len(sessions))
	for i := range sessions {
		s := &sessions[i]
		sv := tui.SessionView{DisplayName: s.DisplayName, Root: s.Root, IsPlain: s.Layout == state.LayoutPlain}
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
				Location:  shortLocation(w.Path),
				Hooked:    present && strings.TrimSpace(info.EmeState) != "",
			}
			// Age/quiet are hook-derived and only meaningful while the agent is actively
			// working or waiting; idle/exited/crashed carry no age.
			if present && info.EmeStateAt > 0 && (status == tui.StatusWorking || status == tui.StatusWaiting) {
				wv.StateChangedAt = time.Unix(info.EmeStateAt, 0)
				wv.AgeLabel = formatAge(now.Sub(wv.StateChangedAt))
				wv.Quiet = wv.Hooked && status == tui.StatusWorking &&
					quietAfter > 0 && now.Sub(wv.StateChangedAt) >= quietAfter
			}
			if status == tui.StatusWorking {
				wv.AgentLabel = agentLabel(w)
			}
			if withDiff {
				if added, deleted, ok := git.DiffStat(w.Path); ok {
					wv.Added, wv.Deleted, wv.HasDiff = added, deleted, true
				}
			}
			sv.Worktrees = append(sv.Worktrees, wv)
		}
		views = append(views, sv)
	}
	return views
}

// formatAge renders a duration as a compact, fixed-meaning token for the row's age cell:
// "" for negative/unknown, then "Ns" (<1m), "Nm" (<1h), "Nh" (<1d), "Nd" otherwise.
func formatAge(d time.Duration) string {
	if d < 0 {
		return ""
	}
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	default:
		return strconv.Itoa(int(d.Hours())/24) + "d"
	}
}

// shellCommands are the foreground process names that mean "the pane is sitting at an
// interactive shell prompt" — i.e. no agent is running. A leading '-' (login shell,
// e.g. -zsh) is trimmed before the lookup. The user's own $SHELL is recognized on top
// of this set (userShellBase), so an unusual shell still reads idle.
var shellCommands = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true,
	"dash": true, "ksh": true, "tcsh": true, "csh": true, "ash": true,
	"nu": true, "nushell": true, "xonsh": true, "elvish": true,
	"pwsh": true, "powershell": true, "osh": true, "ion": true,
}

// isShellCommand reports whether a pane_current_command means the pane is at an
// interactive shell prompt (nothing running in the foreground). An empty/unresolved
// command biases to idle — the safe default: never assert a running agent we cannot
// see (which would also falsely block a launch). The user's own $SHELL basename always
// counts, in addition to the common-shell set.
func isShellCommand(cmd string) bool {
	cmd = strings.TrimPrefix(strings.TrimSpace(cmd), "-")
	if cmd == "" {
		return true
	}
	base := filepath.Base(cmd)
	if base == userShellBase() {
		return true
	}
	return shellCommands[base]
}

// userShellBase is the basename of the user's configured login shell ($SHELL), so a
// pane sitting at that shell's prompt always reads idle even when the shell is not in
// shellCommands. Empty when $SHELL is unset (then only the common set applies).
func userShellBase() string {
	sh := os.Getenv("SHELL")
	if sh == "" {
		return ""
	}
	return filepath.Base(sh)
}

// classifyStatus derives a worktree's agent lifecycle from its pane snapshot. Under
// the child-process launch model the agent runs as a CHILD of the pane's shell (not
// via exec), so the pane stays alive across the agent's exit; "is an agent running"
// is read from the pane's FOREGROUND process, not pane_dead: a shell foreground means
// the prompt is idle, anything else means a command (the agent) is in the foreground.
//
//	window gone, agent ran     → exited
//	pane dead (manual exit), 0 → exited  ○   · non-zero → crashed ✗  (rare now)
//	pane alive, shell prompt   → idle    ·  (ground truth: nothing in the foreground)
//	pane alive, agent running  → @eme_state if a hook pushed one, else working ◐
//
// When an agent hook has stamped @eme_state into the pane (see `eme hooks install`),
// it refines the live non-shell case into working/waiting/done/crashed. A shell
// foreground still wins as idle regardless of @eme_state, so a stale value left by a
// crashed agent (which returns to the shell) can never mask a now-idle pane.
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
		// Rare under the child-process model — only a pane the user manually exited
		// or killed dies; kept so such a pane still classifies sensibly.
		if info.DeadStatus == 0 {
			return tui.StatusExited
		}
		return tui.StatusCrashed
	}
	if isShellCommand(info.Command) {
		return tui.StatusIdle // shell prompt is ground truth: nothing running in the foreground
	}
	if s, ok := emeState(info.EmeState); ok {
		return s // a hook told us the precise sub-state of the running agent
	}
	return tui.StatusWorking
}

// emeState maps a hook-pushed @eme_state value to a status. It is only consulted for a
// live, non-shell pane (something is running); an empty or unrecognized value returns
// ok=false so the caller falls back to the foreground heuristic.
func emeState(v string) (tui.AgentStatus, bool) {
	switch strings.TrimSpace(v) {
	case "working":
		return tui.StatusWorking, true
	case "waiting":
		return tui.StatusWaiting, true
	case "idle", "done":
		return tui.StatusIdle, true
	case "crashed", "error":
		return tui.StatusCrashed, true
	default:
		return tui.StatusIdle, false
	}
}

// shortLocation renders a filesystem path as its last two path segments, prefixed with
// "…/" when more segments precede them. Empty in → empty out. It is width-agnostic; the
// render layer truncates further when the column is narrower than the result.
func shortLocation(path string) string {
	if path == "" {
		return ""
	}
	cleaned := filepath.ToSlash(filepath.Clean(path))
	var parts []string
	for _, p := range strings.Split(cleaned, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	// root-only paths ("/", "//") clean to all-empty components — nothing to show.
	if len(parts) == 0 {
		return ""
	}
	n := 2
	if len(parts) < n {
		n = len(parts)
	}
	tail := strings.Join(parts[len(parts)-n:], "/")
	if len(parts) > 2 {
		return "…/" + tail
	}
	return tail
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
