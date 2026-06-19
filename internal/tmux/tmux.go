// Package tmux wraps tmux operations through a runner for testability.
package tmux

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/jinmu/eme/internal/runner"
)

// Runner is the command runner used by this package. Tests can replace it.
var Runner runner.Runner = runner.Default

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

// AttachSession attaches to a session (and optionally a window) from outside tmux.
func AttachSession(session, windowID string) error {
	target := session
	if windowID != "" {
		target = session + ":" + windowID
	}
	cmd := exec.Command("tmux", "attach-session", "-t", target)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tmux attach-session: %w", err)
	}
	return nil
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

// tmux runs a tmux command and returns stdout/stderr.
func tmux(args ...string) (string, string, error) {
	return Runner.Run(context.Background(), "tmux", args...)
}
