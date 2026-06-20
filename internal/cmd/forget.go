package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var forgetCmd = &cobra.Command{
	Use:   "forget <session>",
	Short: "Remove a project from eme without touching disk or tmux",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return forgetSession(args[0])
	},
}

// forgetSession removes a session from state only. It never deletes files,
// worktrees, or tmux sessions — the disk-safe way to unmanage an adopted clone.
func forgetSession(arg string) error {
	s, err := loadState()
	if err != nil {
		return err
	}
	sess, err := resolveSession(s, arg)
	if err != nil {
		return err
	}
	name := sess.DisplayName
	s.RemoveSession(sess.ID)
	if err := saveState(s); err != nil {
		return err
	}
	fmt.Printf("Forgot %q (no files or tmux sessions were touched)\n", name)
	return nil
}
