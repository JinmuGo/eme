package cmd

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alderwork/eme/internal/git"
	"github.com/alderwork/eme/internal/state"
	"github.com/alderwork/eme/internal/tmux"
	"github.com/alderwork/eme/internal/tui"
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
		sv := tui.SessionView{DisplayName: s.DisplayName, Root: s.Root, IsPlain: s.Layout == state.LayoutPlain, Caffeinate: s.CaffeinateMode}
		for j := range s.Worktrees {
			w := &s.Worktrees[j]
			info, present := snap[w.TmuxWindowID]
			status := classifyStatus(info, present, w.LastAgentCommand)
			status = selfHealIdle(status, info, w, now, quietAfter)
			healed := selfHealWorking(status, info, w, now, quietAfter)
			promoted := healed != status // idle→working: a background task (e.g. a dynamic workflow) is rendering
			status = healed
			wv := tui.WorktreeView{
				Name:      w.Name,
				Branch:    w.Branch,
				SessionID: s.ID,
				IsMain:    w.Name == "main",
				Status:    status,
				Location:  shortLocation(w.Path),
				Hooked:    present && strings.TrimSpace(info.EmeState) != "",
			}
			// Age/quiet: a HOOKED agent derives them from @eme_state_at (when its state last
			// changed); an UN-hooked working agent derives them from window_activity (when its
			// pane last produced output) — the cheap silence signal already in the batched
			// snapshot, no per-pane capture. Either source feeds the SAME soft "quiet" dim; an
			// un-hooked guess never lights the amber beacon (DESIGN.md §5.2/F1) — at most it dims.
			//
			//	hooked   working|waiting → age = now − @eme_state_at ; quiet when working & age ≥ N
			//	un-hooked working        → age = now − window_activity ; quiet when silent ≥ N
			//	everything else          → no age, never quiet
			var stateAt time.Time
			switch {
			case promoted && info.Activity > 0:
				// A self-healed working row (a background workflow is rendering) takes its age from
				// the live output time, so it reads fresh and never trips the quiet dim — its
				// @eme_state_at is the stale Stop, which would wrongly age and dim it.
				stateAt = time.Unix(info.Activity, 0)
			case present && info.EmeStateAt > 0 && (status == tui.StatusWorking || status == tui.StatusWaiting):
				stateAt = time.Unix(info.EmeStateAt, 0)
			case present && !wv.Hooked && status == tui.StatusWorking && info.Activity > 0:
				stateAt = time.Unix(info.Activity, 0)
			}
			if !stateAt.IsZero() {
				wv.StateChangedAt = stateAt
				wv.AgeLabel = formatAge(now.Sub(stateAt))
				wv.Quiet = status == tui.StatusWorking &&
					quietAfter > 0 && now.Sub(stateAt) >= quietAfter
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

// selfHealIdle downgrades a stranded "working" Claude row to idle using window_activity as a
// self-healing fallback for the one gap the hooks cannot cover: an idle agent whose @eme_state
// is a STALE "working" (the Stop hook does NOT fire on an Esc-interrupt — see hooks.go) or is
// still empty before the first prompt. Claude's TUI repaints sub-second (animated spinner +
// ticking elapsed timer) while it is working OR waiting, so a pane that has produced no output
// for longer than the idle threshold is genuinely idle at its prompt — for Claude. It is gated
// to Claude (isClaudeAgent): other agents have no animated TUI, so their silence does not imply
// idle and must keep the optimistic working guess (only dimmed, per buildViews). It only ever
// turns Working→Idle, never toward Waiting, so it can never light the amber beacon on a guess.
// The threshold is 2× quietAfter: a working row stays solid (0–quiet), dims to "quiet"
// (quiet–idle), then reads idle (>idle) — reusing the one Status.QuietAfter knob, no new config.
// With quietAfter disabled (0) or no window_activity stamp, the self-heal is inert.
func selfHealIdle(status tui.AgentStatus, info tmux.PaneInfo, w *state.Worktree, now time.Time, quietAfter time.Duration) tui.AgentStatus {
	if status != tui.StatusWorking || quietAfter <= 0 || info.Activity <= 0 || !isClaudeAgent(info, w) {
		return status
	}
	if now.Sub(time.Unix(info.Activity, 0)) >= 2*quietAfter {
		return tui.StatusIdle
	}
	return status
}

// isClaudeAgent reports whether a worktree's agent is Claude Code — the one agent whose
// continuously-repainting TUI makes a frozen window_activity a reliable idle signal. True when
// the recorded launch command is claude, or when a hook has stamped @eme_state (only Claude
// installs eme's status hooks today, so a present @eme_state implies Claude).
func isClaudeAgent(info tmux.PaneInfo, w *state.Worktree) bool {
	return agentLabel(w) == "claude" || strings.TrimSpace(info.EmeState) != ""
}

// activeRepaintWindow is how recently a Claude pane must have produced output to count as
// "currently repainting". A background dynamic workflow runs in an isolated runtime that fires
// NO hooks while it churns (so the Stop that preceded it leaves @eme_state=idle), yet Claude's
// TUI keeps animating its progress sub-second, so window_activity stays ~0-1s fresh the whole
// time it runs (verified empirically). 10s — 5× the 2s dashboard refresh — clears that with
// margin against snapshot jitter, while keeping the promotion responsive to a workflow ending.
const activeRepaintWindow = 10 * time.Second

// postIdleActivityMargin is how far AFTER the idle stamp the latest repaint must fall for it to
// count as new work rather than the turn's own final render. A Stop hook stamps @eme_state_at
// and the final answer paints at ~the same second, so a genuinely-finished (or freshly-launched,
// via SessionStart) pane has Activity ≈ @eme_state_at; a real background task paints seconds to
// minutes later. This margin is what makes selfHealWorking promote ONLY on activity that
// post-dates the turn, eliminating the bounded just-finished / fresh-launch false positives.
const postIdleActivityMargin = 2 * time.Second

// selfHealWorking is the dual of selfHealIdle: it UPGRADES a Claude row back to Working when a
// hook stamped it idle but the pane is still actively repainting with work that POST-DATES the
// stamp. The gap it covers is the one no hook can: a dynamic workflow (or any background task)
// runs AFTER the turn's Stop fired, in an isolated runtime that emits no hooks while it executes
// — so @eme_state reads "idle"/"done" while real work churns. Because that work animates the TUI
// sub-second, a fresh window_activity is the only available signal, and it is reliable for Claude
// (a genuinely idle Claude prompt freezes window_activity — the same property selfHealIdle relies
// on to demote a stranded agent; a running workflow does not).
//
// It only ever turns Idle→Working (never toward Waiting, so it can't false-light the amber
// beacon), and is tightly gated so it can never resurrect a genuinely-idle row:
//   - quietAfter must be enabled (>0) — the same master switch as selfHealIdle;
//   - the agent must be Claude (isClaudeAgent) — only its TUI repaints while busy;
//   - the idle must be an EXPLICIT hook stamp (@eme_state idle/done) with its @eme_state_at: an
//     empty @eme_state (un-hooked, or before the first prompt) and a shell foreground (the agent
//     EXITED — shell is ground-truth idle) are both left untouched;
//   - the latest repaint must be both RECENT (within activeRepaintWindow → still running) and
//     AFTER the idle stamp by postIdleActivityMargin (→ new work, not the turn's final render).
func selfHealWorking(status tui.AgentStatus, info tmux.PaneInfo, w *state.Worktree, now time.Time, quietAfter time.Duration) tui.AgentStatus {
	if status != tui.StatusIdle || quietAfter <= 0 || info.Activity <= 0 || info.EmeStateAt <= 0 || !isClaudeAgent(info, w) {
		return status
	}
	if isShellCommand(info.Command) {
		return status // a shell prompt is ground-truth idle — the agent exited, never promote
	}
	if s, ok := emeState(info.EmeState); !ok || s != tui.StatusIdle {
		return status // only an explicit hook-stamped idle/done qualifies, not "" or a guess
	}
	activity := time.Unix(info.Activity, 0)
	stamped := time.Unix(info.EmeStateAt, 0)
	// Recent (still running) AND clearly after the stamp (new work, not the turn's final render).
	if now.Sub(activity) < activeRepaintWindow && activity.After(stamped.Add(postIdleActivityMargin)) {
		return tui.StatusWorking
	}
	return status
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
