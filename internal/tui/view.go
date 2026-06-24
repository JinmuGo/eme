package tui

import (
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jinmu/eme/internal/tui/theme"
)

// AgentStatus is the lifecycle state eme surfaces for a worktree's agent.
type AgentStatus int

const (
	StatusIdle    AgentStatus = iota // pane at a shell prompt (no foreground agent)
	StatusWorking                    // a non-shell command (the agent) is in the foreground
	StatusExited                     // window/pane gone after an agent ran, or a clean dead pane
	StatusWaiting                    // waiting for input (reserved; not produced in v1)
	StatusCrashed                    // a pane that died non-zero — a manual kill/exit (rare)
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
// reads with color off; ✗ marks a crash. Under EME_ASCII it degrades to the DESIGN
// §6.4 ASCII set so the glyph channel survives a terminal that can't render the
// Unicode dots.
func (s AgentStatus) Glyph() string {
	if asciiGlyphs() {
		switch s {
		case StatusWaiting:
			return "*"
		case StatusWorking:
			return "o"
		case StatusExited:
			return "."
		case StatusCrashed:
			return "x"
		default:
			return "·" // idle stays the latin-1 middle dot (§6.4): widely available
		}
	}
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

// asciiGlyphs reports whether EME_ASCII is set, opting a non-Unicode terminal into the
// ASCII status glyphs. A tmux popup can't be probed for Unicode capability (the same
// blind spot as background detection, §3.4), so this is an explicit opt-in — a sibling
// of EME_THEME / EME_BEACON_COLOR — read fresh so tests and runtime agree.
func asciiGlyphs() bool {
	return strings.TrimSpace(os.Getenv("EME_ASCII")) != ""
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
// calm. Under the child-process launch model crashed/exited are reached only by a
// pane the user manually kills/exits (an agent that self-exits returns to a live
// shell and reads idle); waiting joins once silence-detection lands.
func (s AgentStatus) NeedsAttention() bool {
	return s == StatusWaiting || s == StatusCrashed
}

// WorktreeView is a render-ready view of one worktree (tmux window).
type WorktreeView struct {
	Name           string
	Branch         string
	SessionID      string
	IsMain         bool
	Status         AgentStatus
	AgentLabel     string // agent binary basename when Working; "" otherwise
	Added          int
	Deleted        int
	HasDiff        bool      // kept; no longer rendered in the row
	Location       string    // compact display path (see cmd.shortLocation)
	Hooked         bool      // a hook pushed @eme_state — status is ground truth, age is known
	StateChangedAt time.Time // from @eme_state_at; zero = unknown (not hooked / not working|waiting)
	Quiet          bool      // hooked + working + age >= quiet_after — a soft "gone quiet" hint
	AgeLabel       string    // formatted age in state ("12m"); "" when StateChangedAt is unknown
}

// SessionView is a render-ready view of one session (folder/project).
type SessionView struct {
	DisplayName string
	Root        string
	// IsPlain marks a plain (non-git) project: the folder is run in place with no
	// git worktree management. The dashboard gates the create-worktree action on
	// it so a plain folder never spawns a child that can only fail.
	IsPlain   bool
	Worktrees []WorktreeView
}
