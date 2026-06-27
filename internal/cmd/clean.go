package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/alderwork/eme/internal/errors"
	"github.com/alderwork/eme/internal/state"
	"github.com/alderwork/eme/internal/tmux"
)

var cleanCmd = &cobra.Command{
	Use:   "clean <session> [worktree]",
	Short: "Clear a finished agent's dead pane, returning the worktree to idle",
	Long: `Revive the worktree's pane back to a fresh shell and clear the recorded agent
so the worktree reads idle again, ready for a new one. Under the child-process launch
model a quit agent already returns to a live shell, so a frozen ("dead") pane is rare —
this recovers one left by a manually killed/exited pane (or a legacy exec'd agent).

It refuses while an agent is still live, so it never clears the record out from under
a running agent. The dashboard binds this to 'x' on a crashed or exited worktree.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireTmuxServer(); err != nil {
			return err
		}
		s, err := loadReconciledState()
		if err != nil {
			return err
		}
		sess, err := resolveSession(s, args[0])
		if err != nil {
			return err
		}
		worktreeName := "main"
		if len(args) == 2 {
			worktreeName = args[1]
		}
		w, err := resolveWorktree(sess, worktreeName)
		if err != nil {
			return err
		}
		if err := cleanWorktree(sess, w); err != nil {
			return err
		}
		if err := saveState(s); err != nil {
			return err
		}
		fmt.Printf("Cleaned %s/%s — pane reset to a fresh shell\n", sess.ID, w.Name)
		return nil
	},
}

// cleanWorktree revives a worktree's dead agent pane to a fresh shell and clears the
// recorded agent so status reads idle. It refuses when an agent is still live —
// clearing the record there would misreport a running agent as idle, because the
// classifier keys "alive pane + a recorded agent" as running. The respawn is
// best-effort: a dead pane revives to a shell, while an absent or already-live pane
// no-ops via the -k-less respawn error (a still-dead pane keeps reading
// crashed/exited, never a false idle).
func cleanWorktree(sess *state.Session, w *state.Worktree) error {
	running, err := agentRunningFn(w)
	if err != nil {
		return err
	}
	if running {
		return errors.New(errors.CodeCommandFailed,
			"An agent is still running in this worktree.",
			"Cleaning would clear the record while the agent is live, misreporting it as idle.",
			"Stop it first (press a), then clean.")
	}
	_ = tmux.RespawnPane(sess.TmuxName, w.TmuxWindowID, w.Path)
	// The revived pane hosts a plain shell, not an exec'd agent, so drop
	// remain-on-exit: otherwise a later normal shell exit re-deads the pane and
	// paints a false exited/crashed beacon. A real agent launch re-sets it on.
	_ = tmux.SetRemainOnExit(sess.TmuxName, w.TmuxWindowID, false)
	w.LastAgentCommand = ""
	w.AgentPID = 0
	return nil
}

// reviveIfDead revives a worktree's pane only when it is dead — frozen by an exec'd
// agent that exited or crashed under remain-on-exit — back to a fresh shell, and
// clears the recorded agent so status reads idle. Unlike cleanWorktree it never
// refuses: a live agent's pane is not dead, so this no-ops there, and likewise on an
// idle shell. Best-effort (a failed snapshot or respawn is ignored). Callers use it
// so switching into an exited worktree lands on a usable shell instead of a "Pane is
// dead" screen; the `p` preview still inspects a dead pane without reviving it.
func reviveIfDead(s *state.State, sess *state.Session, w *state.Worktree) {
	snap, err := tmux.PanesSnapshot()
	if err != nil {
		return
	}
	if info, ok := snap[w.TmuxWindowID]; !ok || !info.Dead {
		return
	}
	// Gate the record clear on the respawn actually succeeding: the -k-less respawn
	// fails harmlessly if the pane is no longer dead (e.g. a concurrent eme already
	// revived it and a fresh agent is live), and clearing then would misreport that
	// live agent as idle.
	if err := tmux.RespawnPane(sess.TmuxName, w.TmuxWindowID, w.Path); err != nil {
		return
	}
	// The revived pane is a plain shell, so drop remain-on-exit (a real agent launch
	// re-sets it on); otherwise a later normal shell exit re-deads it as a false
	// exited/crashed beacon.
	_ = tmux.SetRemainOnExit(sess.TmuxName, w.TmuxWindowID, false)
	if w.LastAgentCommand != "" || w.AgentPID != 0 {
		w.LastAgentCommand = ""
		w.AgentPID = 0
		_ = saveState(s)
	}
}
