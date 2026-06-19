package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jinmu/eme/internal/errors"
	"github.com/jinmu/eme/internal/tmux"
)

var (
	switchDryRun bool
)

var switchCmd = &cobra.Command{
	Use:   "switch <session> [worktree]",
	Short: "Switch to a session or worktree window",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if switchDryRun {
			fmt.Printf("[dry-run] would switch to %s/%s\n", args[0], defaultWorktree(args))
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

		worktreeName := "main"
		if len(args) == 2 {
			worktreeName = args[1]
		}
		w, err := resolveWorktree(sess, worktreeName)
		if err != nil {
			return err
		}

		if os.Getenv("TMUX") != "" {
			if err := tmux.SwitchClient(sess.TmuxName, w.TmuxWindowID); err != nil {
				return errors.Wrap(errors.CodeCommandFailed,
					fmt.Sprintf("Could not switch to %s/%s.", sess.DisplayName, w.Name),
					"tmux switch-client failed.",
					"Verify the tmux session still exists with `tmux list-sessions`.", err)
			}
			return nil
		}

		if err := tmux.AttachSession(sess.TmuxName, w.TmuxWindowID); err != nil {
			return errors.Wrap(errors.CodeCommandFailed,
				fmt.Sprintf("Could not attach to %s/%s.", sess.DisplayName, w.Name),
				"tmux attach-session failed.",
				"Verify the tmux session exists.", err)
		}
		return nil
	},
}

func defaultWorktree(args []string) string {
	if len(args) == 2 {
		return args[1]
	}
	return "main"
}

func init() {
	switchCmd.Flags().BoolVar(&switchDryRun, "dry-run", false, "print planned actions without executing")
}
