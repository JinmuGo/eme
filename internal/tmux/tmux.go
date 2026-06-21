// Package tmux wraps tmux operations through a runner for testability.
package tmux

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jinmu/eme/internal/runner"
)

// Runner is the command runner used by this package. Tests can replace it.
var Runner runner.Runner = runner.Default

// Socket pins every tmux invocation to one server via `tmux -L <Socket>`, so eme
// talks to the same tmux server whether it was launched from a plain shell or
// from a tmux popup hosted on some other server. The empty zero value (used by
// unit tests) means "no -L flag": tmux falls back to its ambient resolution
// ($TMUX when inside tmux, else the default socket). Production sets this from
// config in cmd's PersistentPreRunE (default "default").
var Socket string

// withSocket prepends `-L <Socket>` to args when a socket is pinned.
func withSocket(args []string) []string {
	if Socket == "" {
		return args
	}
	return append([]string{"-L", Socket}, args...)
}

// ManagedSocketPath returns the filesystem path of the tmux server eme is pinned
// to, derived the same way tmux resolves a `-L <name>` socket:
// ${TMUX_TMPDIR:-/tmp}/tmux-<uid>/<name>. It returns "" when no socket is pinned.
func ManagedSocketPath() string {
	if Socket == "" {
		return ""
	}
	dir := os.Getenv("TMUX_TMPDIR")
	if dir == "" {
		dir = "/tmp"
	}
	return filepath.Join(dir, fmt.Sprintf("tmux-%d", os.Getuid()), Socket)
}

// ClientOnManagedServer reports whether the tmux client that launched eme is
// attached to the same server eme manages. Only then does `switch-client` move
// the user's view; otherwise callers must `attach-session` instead. With no
// pinned socket it falls back to "inside tmux", preserving legacy behavior.
func ClientOnManagedServer() bool {
	env := DetectEnv()
	if !env.InsideTmux {
		return false
	}
	if Socket == "" {
		return true
	}
	return resolveSocketPath(env.SocketPath) == resolveSocketPath(ManagedSocketPath())
}

// resolveSocketPath canonicalizes a socket path so comparisons survive symlinked
// temp dirs (notably macOS /tmp -> /private/tmp). It falls back to the raw path.
func resolveSocketPath(p string) string {
	if p == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}

// Env holds tmux environment details.
type Env struct {
	SocketPath string
	InsideTmux bool
}

// DetectEnv reads the tmux environment.
func DetectEnv() Env {
	e := Env{}
	if tmux := os.Getenv("TMUX"); tmux != "" {
		e.InsideTmux = true
		parts := strings.Split(tmux, ",")
		if len(parts) >= 1 {
			e.SocketPath = parts[0]
		}
	}
	return e
}

// Version returns the installed tmux version string.
func Version() (string, error) {
	out, _, err := Runner.Run(context.Background(), "tmux", "-V")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// ServerReachable reports whether the tmux server is running and reachable.
func ServerReachable() bool {
	_, _, err := tmux("list-sessions")
	return err == nil
}

// SessionExists reports whether a tmux session with the given name exists.
func SessionExists(name string) bool {
	_, _, err := tmux("has-session", "-t", name)
	return err == nil
}

// NewSession creates a detached session with an initial window and returns its window id.
func NewSession(name, windowName, cwd string) (string, error) {
	args := []string{"new-session", "-d", "-s", name, "-P", "-F", "#{window_id}"}
	if windowName != "" {
		args = append(args, "-n", windowName)
	}
	if cwd != "" {
		args = append(args, "-c", cwd)
	}
	out, _, err := tmux(args...)
	if err != nil {
		return "", fmt.Errorf("tmux new-session: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// NewWindow creates a new window in the session and returns its window id.
func NewWindow(session, name, cwd string) (string, error) {
	args := []string{"new-window", "-t", session + ":", "-P", "-F", "#{window_id}"}
	if name != "" {
		args = append(args, "-n", name)
	}
	if cwd != "" {
		args = append(args, "-c", cwd)
	}
	out, _, err := tmux(args...)
	if err != nil {
		return "", fmt.Errorf("tmux new-window: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// KillWindow kills a window in a session.
func KillWindow(session, windowID string) error {
	target := session + ":" + windowID
	_, _, err := tmux("kill-window", "-t", target)
	if err != nil {
		return fmt.Errorf("tmux kill-window: %w", err)
	}
	return nil
}

// KillSession kills a tmux session.
func KillSession(name string) error {
	_, _, err := tmux("kill-session", "-t", name)
	if err != nil {
		return fmt.Errorf("tmux kill-session: %w", err)
	}
	return nil
}

// SwitchClient moves the attached tmux client to the given window, switching
// to the window's session if it differs from the current one. Use this (not
// select-window) to take the user to a window that may live in another
// session: `tmux select-window` only changes a session's active window and
// never moves the client between sessions.
func SwitchClient(session, windowID string) error {
	target := session + ":" + windowID
	_, _, err := tmux("switch-client", "-t", target)
	if err != nil {
		return fmt.Errorf("tmux switch-client: %w", err)
	}
	return nil
}

// AttachSession attaches to a session (and optionally a window). Used when the
// caller's client is not on eme's server: from outside tmux, or — with a pinned
// socket — from a popup hosted on a different server. It drops $TMUX so tmux does
// not reject the attach with "sessions should be nested with care"; nesting onto
// a different socket is safe and is the only way to reach a pinned server from a
// popup. With no pin this is a no-op because $TMUX is already unset.
func AttachSession(session, windowID string) error {
	target := session
	if windowID != "" {
		target = session + ":" + windowID
	}
	cmd := exec.Command("tmux", withSocket([]string{"attach-session", "-t", target})...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = envWithoutTMUX(os.Environ())
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tmux attach-session: %w", err)
	}
	return nil
}

// envWithoutTMUX returns env with any TMUX entry removed.
func envWithoutTMUX(env []string) []string {
	out := env[:0:0]
	for _, kv := range env {
		if strings.HasPrefix(kv, "TMUX=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// SendKeys sends literal keys followed by Enter to a target window or pane.
func SendKeys(target, keys string) error {
	if _, _, err := tmux("send-keys", "-t", target, "-l", keys); err != nil {
		return fmt.Errorf("tmux send-keys literal: %w", err)
	}
	if _, _, err := tmux("send-keys", "-t", target, "Enter"); err != nil {
		return fmt.Errorf("tmux send-keys Enter: %w", err)
	}
	return nil
}

// SendKey sends a single tmux key name (e.g. "C-c") to a target window or pane.
func SendKey(target, key string) error {
	if _, _, err := tmux("send-keys", "-t", target, key); err != nil {
		return fmt.Errorf("tmux send-keys %s: %w", key, err)
	}
	return nil
}

// ListSessions returns a map of session names to their first window id.
func ListSessions() (map[string]string, error) {
	out, _, err := tmux("list-sessions", "-F", "#{session_name}\t#{window_id}")
	if err != nil {
		return nil, fmt.Errorf("tmux list-sessions: %w", err)
	}
	sessions := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 {
			sessions[parts[0]] = parts[1]
		}
	}
	return sessions, nil
}

// ListWindows returns a map of window ids to window names for a session.
func ListWindows(session string) (map[string]string, error) {
	out, _, err := tmux("list-windows", "-t", session, "-F", "#{window_id}\t#{window_name}")
	if err != nil {
		return nil, fmt.Errorf("tmux list-windows: %w", err)
	}
	windows := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 {
			windows[parts[0]] = parts[1]
		}
	}
	return windows, nil
}

// PanePID returns the PID of the first pane in a window.
func PanePID(session, windowID string) (int, error) {
	target := session + ":" + windowID
	out, _, err := tmux("list-panes", "-t", target, "-F", "#{pane_pid}")
	if err != nil {
		return 0, fmt.Errorf("tmux list-panes: %w", err)
	}
	pidStr := strings.TrimSpace(out)
	if pidStr == "" {
		return 0, fmt.Errorf("no pane pid found")
	}
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, fmt.Errorf("parse pane pid: %w", err)
	}
	return pid, nil
}

// PaneInfo is a snapshot of one pane's liveness, used to classify agent status.
// Status keys off Dead/DeadStatus (structural) rather than matching Command to the
// agent name, because an interactive agent runs under a different process name
// (e.g. claude runs as `node`, so Command reads "node", not "claude").
type PaneInfo struct {
	Dead       bool
	DeadStatus int    // exit code when Dead; 0 otherwise
	Command    string // pane_current_command — a secondary/label signal only
}

// PanesSnapshot returns liveness for every pane on the server, keyed by window id.
// One batched list-panes call replaces N per-worktree polls. A window maps to its
// first pane (eme runs one pane per agent window).
func PanesSnapshot() (map[string]PaneInfo, error) {
	out, _, err := tmux("list-panes", "-a", "-F",
		"#{window_id}\t#{pane_dead}\t#{pane_dead_status}\t#{pane_current_command}")
	if err != nil {
		return nil, fmt.Errorf("tmux list-panes -a: %w", err)
	}
	snap := make(map[string]PaneInfo)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, "\t", 4)
		if len(f) < 4 {
			continue
		}
		wid := f[0]
		if _, seen := snap[wid]; seen {
			continue // first pane per window wins
		}
		info := PaneInfo{Dead: f[1] == "1", Command: f[3]}
		if info.Dead {
			info.DeadStatus, _ = strconv.Atoi(strings.TrimSpace(f[2]))
		}
		snap[wid] = info
	}
	return snap, nil
}

// SetRemainOnExit keeps a window's pane (and its exit status) alive after the
// process exits, so status can read pane_dead/pane_dead_status. Set per agent
// window at launch — not globally, so only eme's agent panes freeze on exit.
func SetRemainOnExit(session, windowID string) error {
	target := session + ":" + windowID
	if _, _, err := tmux("set-option", "-w", "-t", target, "remain-on-exit", "on"); err != nil {
		return fmt.Errorf("tmux set remain-on-exit: %w", err)
	}
	return nil
}

// RespawnPane revives a dead pane (left by a prior exec'd agent) back to a fresh
// shell in cwd. It is an error to respawn a live pane without -k, so callers use it
// best-effort before relaunch: dead panes revive, live panes no-op via the error.
func RespawnPane(session, windowID, cwd string) error {
	target := session + ":" + windowID
	args := []string{"respawn-pane", "-t", target}
	if cwd != "" {
		args = append(args, "-c", cwd)
	}
	if _, _, err := tmux(args...); err != nil {
		return fmt.Errorf("tmux respawn-pane: %w", err)
	}
	return nil
}

// PopupSize returns the dimensions available for a tmux popup in the current client.
func PopupSize() (width, height int, err error) {
	out, _, err := tmux("display", "-p", "#{popup_width}\t#{popup_height}")
	if err != nil {
		return 0, 0, fmt.Errorf("tmux display: %w", err)
	}
	parts := strings.Split(strings.TrimSpace(out), "\t")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected popup size output: %q", out)
	}
	w, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("parse popup width: %w", err)
	}
	h, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("parse popup height: %w", err)
	}
	return w, h, nil
}

// tmux runs a tmux command and returns stdout/stderr. When Socket is set, the
// invocation is pinned to that server with `-L <Socket>`.
func tmux(args ...string) (string, string, error) {
	return Runner.Run(context.Background(), "tmux", withSocket(args)...)
}
