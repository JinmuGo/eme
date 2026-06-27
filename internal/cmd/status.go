package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/alderwork/eme/internal/tmux"
	"github.com/alderwork/eme/internal/tui"
	"github.com/alderwork/eme/internal/tui/theme"
)

var statusTmux bool

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Print the ambient agent-status segment for a tmux status bar",
	Long: `Print a one-line segment summarizing the agents that need you, for a tmux
status bar. It is empty (a dark cockpit) when nothing needs you, and shows a danger
glyph with a count when agents have crashed — e.g. ✗2. Glyph-led: it reads on a
monochrome or colorblind bar; color is enhancement only.

Wire it in by APPENDING to your existing status-right (eme never edits your config):

    set -g status-interval 2
    set -ga status-right '#(eme status --tmux)'

It is polled by tmux every status-interval seconds — no daemon. A transient read
failure degrades to an empty segment, never an error in your bar.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !statusTmux {
			return cmd.Help()
		}
		fmt.Print(statusSegment())
		return nil
	},
}

func init() {
	statusCmd.Flags().BoolVar(&statusTmux, "tmux", false, "print a single segment for a tmux status bar")
}

// statusSegment builds the ambient segment from a light read: raw state (no full
// reconcile) + the batched pane snapshot, the same cheap path the dashboard ticker
// uses. It NEVER returns an error: a status-bar command that printed one would inject
// it into the user's bar, so any failure degrades to an empty (dark) segment — which
// also honors F1 (a read failure never paints a guessed beacon).
func statusSegment() string {
	st, err := loadState()
	if err != nil {
		return ""
	}
	snap, err := tmux.PanesSnapshot()
	if err != nil {
		return ""
	}
	crashed, waiting := 0, 0
	for i := range st.Sessions {
		for j := range st.Sessions[i].Worktrees {
			w := &st.Sessions[i].Worktrees[j]
			info, present := snap[w.TmuxWindowID]
			switch classifyStatus(info, present, w.LastAgentCommand) {
			case tui.StatusCrashed:
				crashed++
			case tui.StatusWaiting:
				waiting++ // not produced in v1; ready for the T8 silence-detection beacon
			}
		}
	}
	return renderSegment(crashed, waiting)
}

// renderSegment is the pure formatter (no I/O, so it is exhaustively testable).
// Danger beats the beacon — a crash takes the single slot — and the segment is empty
// when nothing needs you. The glyph carries the meaning; the tmux color token is an
// enhancement that a monochrome bar simply ignores.
func renderSegment(crashed, waiting int) string {
	switch {
	case crashed > 0:
		return theme.DangerTmux() + "✗" + strconv.Itoa(crashed) + theme.TmuxReset
	case waiting > 0:
		return theme.BeaconTmux() + "●" + strconv.Itoa(waiting) + theme.TmuxReset
	default:
		return ""
	}
}
