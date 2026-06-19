package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/jinmu/eme/internal/errors"
	"github.com/jinmu/eme/internal/git"
	"github.com/jinmu/eme/internal/state"
	"github.com/jinmu/eme/internal/tmux"
)

var (
	killDryRun bool
)

var killCmd = &cobra.Command{
	Use:   "kill <session> [worktree]",
	Short: "Remove a worktree/window or kill an entire session",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		force, _ := cmd.Flags().GetBool("force")
		if !force && !killDryRun {
			return errors.New(errors.CodeCommandFailed,
				"kill requires --force to confirm removal.",
				"Removing worktrees and tmux windows is destructive.",
				"Run again with --force.")
		}
		if killDryRun {
			if len(args) == 1 {
				fmt.Printf("[dry-run] would kill session %q\n", args[0])
			} else {
				fmt.Printf("[dry-run] would kill worktree %q in session %q\n", args[1], args[0])
			}
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

		if len(args) == 1 {
			return killSession(s, sess)
		}
		return killWorktree(s, sess, args[1])
	},
}

func killSession(s *state.State, sess *state.Session) error {
	for _, w := range sess.Worktrees {
		if w.Name == "main" {
			continue
		}
		if err := os.RemoveAll(w.Path); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not remove %s: %v\n", w.Path, err)
		}
	}
	if err := tmux.KillSession(sess.TmuxName); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not kill tmux session %s: %v\n", sess.TmuxName, err)
	}
	if err := os.RemoveAll(filepath.Join(sess.Root, "main")); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not remove main worktree: %v\n", err)
	}
	if err := os.RemoveAll(filepath.Join(sess.Root, ".bare")); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not remove bare repo: %v\n", err)
	}
	s.RemoveSession(sess.ID)
	if err := saveState(s); err != nil {
		return err
	}
	fmt.Printf("Killed session %q\n", sess.DisplayName)
	return nil
}

func killWorktree(s *state.State, sess *state.Session, name string) error {
	if name == "main" {
		return errors.New(errors.CodeCommandFailed,
			"Cannot remove the main worktree.",
			"The main worktree is tied to the project session.",
			"Use `eme kill <session> --force` to remove the whole project.")
	}

	w, err := resolveWorktree(sess, name)
	if err != nil {
		return err
	}

	if err := git.WorktreeRemove(w.Path, true); err != nil {
		fmt.Fprintf(os.Stderr, "warning: git worktree remove failed: %v\n", err)
	}
	if err := os.RemoveAll(w.Path); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not remove %s: %v\n", w.Path, err)
	}
	if err := tmux.KillWindow(sess.TmuxName, w.TmuxWindowID); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not kill tmux window: %v\n", err)
	}
	sess.RemoveWorktree(name)
	if err := saveState(s); err != nil {
		return err
	}
	fmt.Printf("Killed worktree %q in session %q\n", name, sess.DisplayName)
	return nil
}

func init() {
	killCmd.Flags().BoolP("force", "f", false, "confirm destructive removal")
	killCmd.Flags().BoolVar(&killDryRun, "dry-run", false, "print planned actions without executing")
}
