package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/jinmu/eme/internal/config"
	"github.com/jinmu/eme/internal/errors"
	"github.com/jinmu/eme/internal/state"
	"github.com/jinmu/eme/internal/tmux"
	"github.com/jinmu/eme/internal/tui"
)

var (
	agentDryRun bool
	agentPick   bool
)

// lookPath resolves an agent binary (swapped in tests). It defaults to a resolver
// enriched with the user's login-shell PATH: eme usually runs inside a tmux popup,
// which inherits the tmux server's minimal PATH — often missing per-user bin dirs
// like ~/.local/bin (where Claude Code installs `claude`). A plain exec.LookPath
// there misreports such agents as "not installed", and the agent's own pane (a login
// shell) could in fact run them. Enriching the lookup keeps detection and the launch
// pre-check in step with what the pane can actually execute.
var lookPath = enrichedLookPath

// enrichedLookPath resolves bin on the process PATH first, then on the user's
// login-shell PATH, so an agent that is on the interactive PATH but not the popup's
// minimal PATH still resolves.
func enrichedLookPath(bin string) (string, error) {
	if p, err := exec.LookPath(bin); err == nil {
		return p, nil
	}
	if p, ok := findOnPath(bin, loginShellPATH()); ok {
		return p, nil
	}
	return "", fmt.Errorf("%q not found on PATH", bin)
}

var (
	loginPathOnce sync.Once
	loginPathVal  string
)

// loginShellPATH returns the PATH the user's login + interactive shell would build
// (sourcing .zprofile/.zshrc, where tools like mise and ~/.local/bin are added),
// captured once per process. Empty when it cannot be determined.
func loginShellPATH() string {
	loginPathOnce.Do(func() { loginPathVal = captureLoginShellPATH() })
	return loginPathVal
}

// captureLoginShellPATH runs the user's shell as a login + interactive shell and
// reads back its $PATH, fenced by unit-separator bytes so any banner the rc files
// print is ignored. Best-effort with a short timeout; "" on any failure.
func captureLoginShellPATH() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-lic", `printf '\037%s\037' "$PATH"`)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	const sep = "\x1f" // ASCII unit separator — never appears in a PATH
	_, after, ok := strings.Cut(string(out), sep)
	if !ok {
		return ""
	}
	val, _, ok := strings.Cut(after, sep)
	if !ok {
		return ""
	}
	return val
}

// findOnPath looks bin up across the directories in pathList (a PATH-style string),
// returning the first executable match.
func findOnPath(bin, pathList string) (string, bool) {
	if pathList == "" {
		return "", false
	}
	if strings.ContainsRune(bin, filepath.Separator) {
		if isExecutableFile(bin) {
			return bin, true
		}
		return "", false
	}
	for _, dir := range filepath.SplitList(pathList) {
		if dir == "" {
			continue
		}
		if p := filepath.Join(dir, bin); isExecutableFile(p) {
			return p, true
		}
	}
	return "", false
}

// isExecutableFile reports whether p is a regular file with an executable bit set.
func isExecutableFile(p string) bool {
	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}

// pickAgent runs the interactive agent picker. Swapped in tests.
var pickAgent = runAgentPicker

// sendKeys is the tmux send-keys seam (swapped in tests).
var sendKeys = tmux.SendKeys

// agentRunningFn reports whether an agent is live in a worktree's pane (seam for tests).
var agentRunningFn = agentRunning

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
	// Refuse before showing the picker if an agent is live in the pane (keyed off
	// pane_dead). Best-effort: if pane state is unreadable, fall through — the launch
	// path's own guard is the backstop.
	if running, err := agentRunningFn(w); err == nil && running {
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
	if _, err := lookPath(binary); err != nil {
		return errors.New(errors.CodeAgentNotFound,
			fmt.Sprintf("Configured agent %q not found on PATH.", binary),
			"The agent binary is not executable or not installed.",
			"Install it or set agent.command in ~/.config/eme/config.toml.")
	}

	// Defense in depth: never send a command into a pane whose foreground is already a
	// live agent (or any running command) — it would land as literal keystrokes in that
	// TUI. Best-effort: if pane state is unreadable, proceed with the user's explicit
	// launch.
	if running, err := agentRunningFn(w); err == nil && running {
		return errors.New(errors.CodeCommandFailed,
			"An agent is already running in this worktree.",
			"Sending a command would type into the running agent's pane.",
			"Stop it first (press a), then launch again.")
	}

	target := sess.TmuxName + ":" + w.TmuxWindowID

	// Revive the pane to a fresh shell if a prior one was left dead (best-effort: a
	// live pane no-ops). Drop remain-on-exit so the shell that now hosts the agent
	// closes normally on exit instead of freezing into a dead pane.
	_ = tmux.RespawnPane(sess.TmuxName, w.TmuxWindowID, w.Path)
	_ = tmux.SetRemainOnExit(sess.TmuxName, w.TmuxWindowID, false)
	// Clear any @eme_state a previous agent's hook left on this pane, so the dashboard
	// reads the foreground heuristic until the new agent's first hook fires (rather than
	// a stale waiting/idle from the last session).
	_ = tmux.SetAgentState(sess.TmuxName, w.TmuxWindowID, "")

	// Run the agent as a CHILD of the pane's shell (no exec): the shell survives the
	// agent's exit, so quitting the agent returns to a prompt instead of a dead pane.
	// Liveness/status then read the pane's foreground process, not pane_dead.
	if err := sendKeys(target, command); err != nil {
		return errors.Wrap(errors.CodeCommandFailed,
			"Could not send agent command to tmux pane.",
			"tmux send-keys failed.",
			"Verify the tmux window still exists.", err)
	}

	// Record the agent name for the status label. Liveness is read from the pane's
	// foreground process, not a pid, so we deliberately record NO pid: the pane process
	// is the shell and the agent is its child, so a pane pid would mislead.
	w.LastAgentCommand = command
	if err := saveState(s); err != nil {
		return err
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

	// Decide stop-vs-launch off the pane's foreground process, not a recorded pid: the
	// agent runs as a child of the shell, so a non-shell foreground means an agent is
	// active (→ stop with C-c, which returns the pane to its shell) and a shell prompt
	// means none is (→ launch). This keeps a launch from typing into a live agent.
	running, err := agentRunningFn(w)
	if err != nil {
		return err
	}
	if running {
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
		// C-c is a gentle interrupt an interactive agent may trap; report honestly
		// rather than asserting it stopped. If it survives, the next toggle interrupts
		// it again — it never falls through to relaunch into a live pane.
		fmt.Printf("Sent interrupt to agent in %s/%s\n", sess.DisplayName, w.Name)
		return nil
	}

	return launchAgentCommand(s, sess, w, command)
}

// agentRunning reports whether something is running in the worktree pane's
// foreground. Under the child-process launch model the agent runs as a child of the
// pane's shell, so the pane survives the agent's exit; liveness is read from the
// FOREGROUND process (pane_current_command), not pane_dead or a recorded pid. The
// pane is "running" iff it is present, not dead, and its foreground is not an
// interactive shell — which keeps stop/launch decisions from typing a command into a
// pane that already has the agent (or any command) in the foreground.
func agentRunning(w *state.Worktree) (bool, error) {
	snap, err := tmux.PanesSnapshot()
	if err != nil {
		return false, errors.Wrap(errors.CodeCommandFailed,
			"Could not read agent status.",
			"Reading tmux pane state failed.",
			"Verify the tmux server is reachable.", err)
	}
	info, present := snap[w.TmuxWindowID]
	return present && !info.Dead && !isShellCommand(info.Command), nil
}

func init() {
	agentCmd.Flags().BoolVar(&agentDryRun, "dry-run", false, "print planned actions without executing")
	agentCmd.Flags().BoolVar(&agentPick, "pick", false, "choose the agent for this worktree from the catalog")
}
