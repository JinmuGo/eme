package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/jinmu/eme/internal/errors"
	"github.com/jinmu/eme/internal/state"
	"github.com/jinmu/eme/internal/tmux"
	"github.com/jinmu/eme/internal/tui"
)

// caffeinateWindowName is the hidden tmux window that hosts a session's caffeinate
// daemon. Detection and teardown key off this exact name.
const caffeinateWindowName = "__eme_caffeinate"

// caffeinatePollInterval is how often auto-mode re-samples the session's agents.
const caffeinatePollInterval = 3 * time.Second

// caffeinateSupportedFn reports whether this platform supports caffeinate. A var
// seam so tests can force it on regardless of the host OS.
var caffeinateSupportedFn = func() bool { return runtime.GOOS == "darwin" }

// anyWorking reports whether any of the session's panes classify as a working agent.
func anyWorking(statuses []tui.AgentStatus) bool {
	for _, s := range statuses {
		if s == tui.StatusWorking {
			return true
		}
	}
	return false
}

// shouldAssert is the pure auto-mode decision: hold caffeinate while an agent is
// working, or for `grace` after the last working sample. Manual mode never calls
// this (it asserts unconditionally).
func shouldAssert(working bool, sinceLastWorking, grace time.Duration) bool {
	return working || sinceLastWorking < grace
}

// normalizeMode validates/normalizes a --mode value to off|manual|auto.
func normalizeMode(m string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(m)) {
	case "off":
		return "off", nil
	case "manual":
		return "manual", nil
	case "auto":
		return "auto", nil
	default:
		return "", errors.New(errors.CodeCommandFailed,
			fmt.Sprintf("invalid caffeinate mode %q.", m),
			"Mode must be one of: off, manual, auto.",
			"Run `eme caffeinate <session> --mode manual` (or auto/off).")
	}
}

// emeExecutable resolves the running eme binary's absolute path. A seam for tests.
var emeExecutable = os.Executable

// armCaffeinate (re)starts the session's hidden caffeinate window in the given mode.
// It first disarms any existing window so a mode change takes effect, then spawns a
// fresh daemon by absolute eme path (PATH-independent). Bound to the tmux session:
// when the session dies the window dies and the daemon's caffeinate child dies with
// it. No-op off macOS.
func armCaffeinate(sess *state.Session, mode string) error {
	if !caffeinateSupportedFn() {
		return nil
	}
	_ = disarmCaffeinate(sess) // drop any stale/previous-mode window first (best-effort)
	bin, err := emeExecutable()
	if err != nil {
		return errors.New(errors.CodeCommandFailed,
			"could not locate the eme binary to start caffeinate.",
			err.Error(),
			"Reinstall eme or report this if it persists.")
	}
	if _, err := tmux.NewWindowCmd(sess.TmuxName, caffeinateWindowName, sess.MainPath(),
		bin, "caffeinate-daemon", sess.ID, "--mode", mode); err != nil {
		return errors.New(errors.CodeCommandFailed,
			"could not start the caffeinate window.",
			err.Error(),
			"Make sure the session's tmux server is reachable.")
	}
	return nil
}

// disarmCaffeinate kills the session's caffeinate window by name (best-effort: a
// missing window is fine). Killing the window SIGHUPs the daemon + its caffeinate
// child, releasing the assertion.
func disarmCaffeinate(sess *state.Session) error {
	return tmux.KillWindow(sess.TmuxName, caffeinateWindowName)
}

// setCaffeinate applies a normalized mode (off|manual|auto) to a session: it arms or
// disarms the window FIRST (so persisted intent never claims a state tmux isn't in),
// then records the intent and saves. off → "" intent.
func setCaffeinate(s *state.State, sess *state.Session, mode string) error {
	switch mode {
	case "off":
		_ = disarmCaffeinate(sess)
		sess.CaffeinateMode = ""
	case "manual", "auto":
		if err := armCaffeinate(sess, mode); err != nil {
			return err
		}
		sess.CaffeinateMode = mode
	}
	return saveState(s)
}

var caffeinateMode string       // --mode for `eme caffeinate`
var caffeinateDaemonMode string // --mode for the hidden daemon

var caffeinateCmd = &cobra.Command{
	Use:   "caffeinate <session> [worktree]",
	Short: "Keep the Mac awake for a session (macOS): --mode manual|auto|off",
	Long: `Designate a session to prevent macOS from sleeping while it runs.

  manual  keep awake for the whole session, unconditionally
  auto    keep awake only while an agent is working in the session
  off      stop keeping awake

The assertion lives in a hidden tmux window inside the session, so it stops
automatically when the session ends. macOS only; a no-op elsewhere.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		mode, err := normalizeMode(caffeinateMode)
		if err != nil {
			return err
		}
		if !caffeinateSupportedFn() {
			fmt.Println("caffeinate is only supported on macOS — no-op.")
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
		if len(args) == 2 { // a worktree arg only validates; caffeinate is session-scoped
			if _, err := resolveWorktree(sess, args[1]); err != nil {
				return err
			}
		}
		if err := setCaffeinate(s, sess, mode); err != nil {
			return err
		}
		if mode == "off" {
			fmt.Printf("caffeinate off for %s\n", sess.DisplayName)
		} else {
			fmt.Printf("caffeinate %s for %s\n", mode, sess.DisplayName)
		}
		return nil
	},
}

var caffeinateDaemonCmd = &cobra.Command{
	Use:    "caffeinate-daemon <session-id>",
	Short:  "internal: per-session caffeinate supervisor (runs inside a tmux window)",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mode, err := normalizeMode(caffeinateDaemonMode)
		if err != nil {
			return err
		}
		return runCaffeinateDaemon(args[0], mode)
	},
}

// sessionStatuses classifies every worktree pane of a session, reusing the dashboard's
// classifyStatus (foreground process + @eme_state). The hidden caffeinate window is not
// a state worktree, so it is never counted. Best-effort: any read failure yields nil.
func sessionStatuses(sessionID string) []tui.AgentStatus {
	s, err := loadState()
	if err != nil {
		return nil
	}
	sess := s.SessionByID(sessionID)
	if sess == nil {
		return nil
	}
	snap, err := tmux.PanesSnapshot()
	if err != nil {
		return nil
	}
	out := make([]tui.AgentStatus, 0, len(sess.Worktrees))
	for i := range sess.Worktrees {
		w := &sess.Worktrees[i]
		info, present := snap[w.TmuxWindowID]
		out = append(out, classifyStatus(info, present, w.LastAgentCommand))
	}
	return out
}

// caffeinator owns a single /usr/bin/caffeinate child, started/stopped idempotently.
type caffeinator struct {
	flags []string
	cmd   *exec.Cmd
}

func (c *caffeinator) start() {
	if c.cmd != nil {
		return
	}
	cmd := exec.Command("/usr/bin/caffeinate", c.flags...)
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "caffeinate: start failed: %v\n", err)
		return
	}
	c.cmd = cmd
}

func (c *caffeinator) stop() {
	if c.cmd == nil {
		return
	}
	_ = c.cmd.Process.Kill()
	_, _ = c.cmd.Process.Wait()
	c.cmd = nil
}

// runCaffeinateDaemon is the in-window supervisor. It holds a caffeinate child and,
// in auto mode, polls the session's agent statuses to assert/release. It exits on
// SIGHUP/SIGTERM/SIGINT (tmux SIGHUPs the pane group when the window/session dies),
// releasing the assertion via the deferred stop. No-op off macOS.
func runCaffeinateDaemon(sessionID, mode string) error {
	if !caffeinateSupportedFn() {
		return nil
	}
	caf := &caffeinator{flags: cfg.CaffeinateFlags()}
	defer caf.stop()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)

	if mode == "manual" {
		caf.start()
		<-sigs
		return nil
	}

	// auto: assert while working, releasing AutoGraceDuration after the last working sample.
	grace := cfg.AutoGraceDuration()
	var sinceLast time.Duration = grace + time.Second // start released
	ticker := time.NewTicker(caffeinatePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-sigs:
			return nil
		case <-ticker.C:
			working := anyWorking(sessionStatuses(sessionID))
			if working {
				sinceLast = 0
			} else {
				sinceLast += caffeinatePollInterval
			}
			if shouldAssert(working, sinceLast, grace) {
				caf.start()
			} else {
				caf.stop()
			}
		}
	}
}

func init() {
	caffeinateCmd.Flags().StringVar(&caffeinateMode, "mode", "", "manual | auto | off")
	_ = caffeinateCmd.MarkFlagRequired("mode")
	caffeinateDaemonCmd.Flags().StringVar(&caffeinateDaemonMode, "mode", "", "manual | auto")
}
