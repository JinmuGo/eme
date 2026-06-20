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
	StatusCrashed                    // agent exited non-zero (reserved; needs exit-code capture — see DESIGN.md §5.4)
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
		return "working"
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
// Per DESIGN.md §5.4 the target is waiting || crashed — clean exits should recede.
// That split is gated on exit-code capture in the runner (not yet built), so until
// crashed is produced, exited still counts here to keep the tally meaningful.
func (s AgentStatus) NeedsAttention() bool {
	return s == StatusWaiting || s == StatusCrashed || s == StatusExited
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
