package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/jinmu/eme/internal/config"
	"github.com/jinmu/eme/internal/errors"
	"github.com/jinmu/eme/internal/runner"
	"github.com/jinmu/eme/internal/state"
	"github.com/jinmu/eme/internal/tmux"
	"github.com/jinmu/eme/internal/tui"
)

var (
	agentDryRun bool
	agentPick   bool
)

// lookPath is the PATH lookup seam (swapped in tests).
var lookPath = exec.LookPath

// pickAgent runs the interactive agent picker. Swapped in tests.
var pickAgent = runAgentPicker

// sendKeys is the tmux send-keys seam (swapped in tests).
var sendKeys = tmux.SendKeys

// runAgentPicker shows the agent picker as a full-screen bubbletea program.
func runAgentPicker(items []tui.AgentItem, defaultName string) (tui.AgentItem, bool, bool, error) {
	picker := tui.NewAgentPicker(items, defaultName)
	if _, err := tea.NewProgram(picker, tea.WithAltScreen()).Run(); err != nil {
		return tui.AgentItem{}, false, false, fmt.Errorf("agent picker: %w", err)
	}
	if picker.Cancelled() {
		return tui.AgentItem{}, false, true, nil
	}
	sel, ok := picker.Chosen()
	if !ok {
		return tui.AgentItem{}, false, true, nil // closed without choosing
	}
	return sel, sel.None, false, nil
}

// agentItems builds picker rows from a catalog, marking PATH-installed agents
// selectable and appending a trailing "none" row.
func agentItems(catalog []config.AgentSpec) []tui.AgentItem {
	items := make([]tui.AgentItem, 0, len(catalog)+1)
	for _, a := range catalog {
		bin := a.Command
		if fields := strings.Fields(a.Command); len(fields) > 0 {
			bin = fields[0]
		}
		_, err := lookPath(bin)
		items = append(items, tui.AgentItem{Name: a.Name, Command: a.Command, Installed: err == nil})
	}
	items = append(items, tui.AgentItem{Name: "none", None: true, Installed: true})
	return items
}

// countInstalled counts selectable, non-none rows.
func countInstalled(items []tui.AgentItem) int {
	n := 0
	for _, it := range items {
		if it.Installed && !it.None {
			n++
		}
	}
	return n
}

// defaultAgentName returns the catalog name whose command (or name) matches the
// given command, for pre-highlighting the picker. Empty when no match.
func defaultAgentName(catalog []config.AgentSpec, command string) string {
	for _, a := range catalog {
		if a.Command == command || a.Name == command {
			return a.Name
		}
	}
	return ""
}

// pickWorktreeAgent runs the agent picker for w and records the choice as the
// worktree's override. It refuses while an agent is already running, because the
// picked command would otherwise be typed into the running agent's pane.
func pickWorktreeAgent(s *state.State, sess *state.Session, w *state.Worktree) error {
	if w.AgentPID > 0 && processExists(w.AgentPID) {
		return errors.New(errors.CodeCommandFailed,
			"An agent is already running in this worktree.",
			"Choosing a new agent would type into the running one.",
			"Stop it first (press a), then choose a new one.")
	}
	return chooseAndLaunchAgent(s, sess, w, resolvedAgentCommand(sess, w), func(command string) {
		w.AgentCommandOverride = command
	})
}

// chooseAndLaunchAgent shows the agent picker (pre-highlighting defaultCmd) and,
// on a concrete selection, calls apply(command), persists state, and launches it
// in w. "none", cancel, or an empty catalog leave everything untouched.
func chooseAndLaunchAgent(s *state.State, sess *state.Session, w *state.Worktree, defaultCmd string, apply func(command string)) error {
	catalog := cfg.Catalog()
	items := agentItems(catalog)
	if countInstalled(items) == 0 {
		fmt.Println("No agents found on PATH. Install claude, codex, gemini, or opencode, or set agent.command in ~/.config/eme/config.toml.")
		return nil
	}
	sel, none, cancelled, err := pickAgent(items, defaultAgentName(catalog, defaultCmd))
	if err != nil || none || cancelled {
		return err
	}
	apply(sel.Command)
	if err := saveState(s); err != nil {
		return err
	}
	return launchAgentCommand(s, sess, w, sel.Command)
}

// resolvedAgentCommand resolves the effective agent command for a worktree:
// the worktree override, then the session default, then the global config.
func resolvedAgentCommand(sess *state.Session, w *state.Worktree) string {
	if w.AgentCommandOverride != "" {
		return w.AgentCommandOverride
	}
	if sess.AgentCommand != "" {
		return sess.AgentCommand
	}
	return cfg.Agent.Command
}

// launchAgentCommand starts command in the worktree's tmux window. The window's
// cwd is already the worktree, so the bare command is sent with no path
// argument (which is what makes claude/codex/gemini work, not just opencode).
func launchAgentCommand(s *state.State, sess *state.Session, w *state.Worktree, command string) error {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return errors.New(errors.CodeAgentNotFound,
			"No agent command configured.",
			"The resolved agent command is empty.",
			"Set agent.command in ~/.config/eme/config.toml.")
	}
	binary := fields[0]
	if _, _, err := runner.Default.Run(context.Background(), "which", binary); err != nil {
		return errors.New(errors.CodeAgentNotFound,
			fmt.Sprintf("Configured agent %q not found on PATH.", binary),
			"The agent binary is not executable or not installed.",
			"Install it or set agent.command in ~/.config/eme/config.toml.")
	}

	target := sess.TmuxName + ":" + w.TmuxWindowID
	if err := sendKeys(target, command); err != nil {
		return errors.Wrap(errors.CodeCommandFailed,
			"Could not send agent command to tmux pane.",
			"tmux send-keys failed.",
			"Verify the tmux window still exists.", err)
	}

	// Best-effort: record pane PID as agent PID.
	if pid, err := tmux.PanePID(sess.TmuxName, w.TmuxWindowID); err == nil {
		w.AgentPID = pid
		w.LastAgentCommand = command
		if err := saveState(s); err != nil {
			return err
		}
	}

	fmt.Printf("Started agent in %s/%s\n", sess.DisplayName, w.Name)
	return nil
}

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

		if agentPick {
			return pickWorktreeAgent(s, sess, w)
		}

		return toggleAgent(s, sess, w)
	},
}

func toggleAgent(s *state.State, sess *state.Session, w *state.Worktree) error {
	command := resolvedAgentCommand(sess, w)
	if command == "" {
		return errors.New(errors.CodeAgentNotFound,
			"No agent command configured.",
			"Neither session, worktree, nor global config specifies an agent command.",
			"Set agent.command in ~/.config/eme/config.toml.")
	}

	// If we have a recorded PID and it is alive, stop it by sending Ctrl+C.
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

	return launchAgentCommand(s, sess, w, command)
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
	agentCmd.Flags().BoolVar(&agentPick, "pick", false, "choose the agent for this worktree from the catalog")
}
