package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/jinmu/eme/internal/tui/theme"
)

// AgentStatus is the lifecycle state eme surfaces for a worktree's agent.
type AgentStatus int

const (
	StatusIdle    AgentStatus = iota // no agent has run
	StatusWorking                    // agent process is alive
	StatusExited                     // agent ran and exited cleanly
	StatusWaiting                    // waiting for input (reserved; not produced in v1)
	StatusCrashed                    // agent exited non-zero (exec replaces the shell → #{pane_dead_status})
)

// statusStyle maps each status to its glyph+label style, per DESIGN.md §5. The
// beacon (waiting) and crashed fire every channel at once: hue + bold + glyph.
var statusStyle = map[AgentStatus]lipgloss.Style{
	StatusWaiting: lipgloss.NewStyle().Foreground(theme.Beacon).Bold(true),
	StatusWorking: lipgloss.NewStyle().Foreground(theme.Working),
	StatusExited:  lipgloss.NewStyle().Foreground(theme.Exited),
	StatusIdle:    lipgloss.NewStyle().Foreground(theme.Idle),
	StatusCrashed: lipgloss.NewStyle().Foreground(theme.Danger).Bold(true),
}

// Glyph returns the status dot. The progression ● ◐ ○ · is a fullness ramp that
// reads with color off; ✗ marks a crash.
func (s AgentStatus) Glyph() string {
	switch s {
	case StatusWaiting:
		return "●"
	case StatusWorking:
		return "◐"
	case StatusExited:
		return "○"
	case StatusCrashed:
		return "✗"
	default:
		return "·"
	}
}

// Label returns the status word.
func (s AgentStatus) Label() string {
	switch s {
	case StatusWaiting:
		return "waiting"
	case StatusWorking:
		// v1 interim: the alive state lumps working|waiting (the runtime can't yet
		// tell them apart — DESIGN.md §5.2). Honest label until silence-detection
		// splits out waiting, when this flips back to "working".
		return "running"
	case StatusExited:
		return "exited"
	case StatusCrashed:
		return "crashed"
	default:
		return "idle"
	}
}

// NeedsAttention reports whether this status counts toward the header tally.
//
// Per DESIGN.md §5.4 this is waiting || crashed — clean exits recede, running is
// calm. crashed is now produced (exec + #{pane_dead_status}), so exited no longer
// counts; waiting joins once silence-detection lands.
func (s AgentStatus) NeedsAttention() bool {
	return s == StatusWaiting || s == StatusCrashed
}

// WorktreeView is a render-ready view of one worktree (tmux window).
type WorktreeView struct {
	Name       string
	Branch     string
	SessionID  string
	IsMain     bool
	Status     AgentStatus
	AgentLabel string // agent binary basename when Working; "" otherwise
	Added      int
	Deleted    int
	HasDiff    bool
}

// SessionView is a render-ready view of one session (folder/project).
type SessionView struct {
	DisplayName string
	Root        string
	Worktrees   []WorktreeView
}
