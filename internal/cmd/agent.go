package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/jinmu/eme/internal/errors"
	"github.com/jinmu/eme/internal/runner"
	"github.com/jinmu/eme/internal/state"
	"github.com/jinmu/eme/internal/tmux"
)

var (
	agentDryRun bool
)

var agentCmd = &cobra.Command{
	Use:   "agent <session> [worktree]",
	Short: "Start or stop an AI agent in a worktree",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		worktreeName := "main"
		if len(args) == 2 {
			worktreeName = args[1]
		}
		if agentDryRun {
			fmt.Printf("[dry-run] would toggle agent in %s/%s\n", args[0], worktreeName)
			return nil
		}
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

		w, err := resolveWorktree(sess, worktreeName)
		if err != nil {
			return err
		}

		return toggleAgent(s, sess, w)
	},
}

func toggleAgent(s *state.State, sess *state.Session, w *state.Worktree) error {
	// Determine agent command.
	agentCmd := w.AgentCommandOverride
	if agentCmd == "" {
		agentCmd = sess.AgentCommand
	}
	if agentCmd == "" {
		agentCmd = cfg.Agent.Command
	}
	if agentCmd == "" {
		return errors.New(errors.CodeAgentNotFound,
			"No agent command configured.",
			"Neither session, worktree, nor global config specifies an agent command.",
			"Set agent.command in ~/.config/eme/config.toml.")
	}

	// If we have a recorded PID and it is alive, stop it by sending Ctrl+C to the pane.
	if w.AgentPID > 0 && processExists(w.AgentPID) {
		target := sess.TmuxName + ":" + w.TmuxWindowID
		if err := tmux.SendKey(target, "C-c"); err != nil {
			return errors.Wrap(errors.CodeCommandFailed,
				"Could not stop agent.",
				"Sending Ctrl+C to the agent pane failed.",
				"You may need to stop it manually from tmux.", err)
		}
		w.AgentPID = 0
		if err := saveState(s); err != nil {
			return err
		}
		fmt.Printf("Stopped agent in %s/%s\n", sess.DisplayName, w.Name)
		return nil
	}

	// Verify agent binary exists.
	binary := strings.Fields(agentCmd)[0]
	if _, _, err := runner.Default.Run(context.Background(), "which", binary); err != nil {
		return errors.New(errors.CodeAgentNotFound,
			fmt.Sprintf("Configured agent %q not found on PATH.", binary),
			"The agent binary is not executable or not installed.",
			"Install it or set agent.command in ~/.config/eme/config.toml.")
	}

	target := sess.TmuxName + ":" + w.TmuxWindowID
	cmdLine := agentCmd + " " + w.Path
	if err := tmux.SendKeys(target, cmdLine); err != nil {
		return errors.Wrap(errors.CodeCommandFailed,
			"Could not send agent command to tmux pane.",
			"tmux send-keys failed.",
			"Verify the tmux window still exists.", err)
	}

	// Best-effort: record pane PID as agent PID.
	pid, err := tmux.PanePID(sess.TmuxName, w.TmuxWindowID)
	if err == nil {
		w.AgentPID = pid
		w.LastAgentCommand = agentCmd
		if err := saveState(s); err != nil {
			return err
		}
	}

	fmt.Printf("Started agent in %s/%s\n", sess.DisplayName, w.Name)
	return nil
}

func processExists(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

func killProcess(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Signal(os.Interrupt)
}

func init() {
	agentCmd.Flags().BoolVar(&agentDryRun, "dry-run", false, "print planned actions without executing")
}
