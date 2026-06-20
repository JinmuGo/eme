package tui

import "github.com/charmbracelet/lipgloss"

// AgentStatus is the lifecycle state eme surfaces for a worktree's agent.
type AgentStatus int

const (
	StatusIdle    AgentStatus = iota // no agent has run
	StatusWorking                    // agent process is alive
	StatusExited                     // agent ran and is no longer alive
	StatusWaiting                    // waiting for input (reserved; not produced in v1)
)

// statusStyle is the lipgloss style for each status glyph+label.
var statusStyle = map[AgentStatus]lipgloss.Style{
	StatusWaiting: lipgloss.NewStyle().Foreground(lipgloss.Color("#E5C07B")).Bold(true),
	StatusWorking: lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")),
	StatusExited:  lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")),
	StatusIdle:    lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")),
}

// Glyph returns the status dot.
func (s AgentStatus) Glyph() string {
	switch s {
	case StatusWaiting:
		return "●"
	case StatusWorking:
		return "◐"
	case StatusExited:
		return "○"
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
	default:
		return "idle"
	}
}

// NeedsAttention reports whether this status counts toward the "N needs you"
// header — an agent waiting for input or one that has exited.
func (s AgentStatus) NeedsAttention() bool {
	return s == StatusWaiting || s == StatusExited
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
