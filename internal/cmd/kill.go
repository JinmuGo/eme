package cmd

import (
	"context"
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
		return killWorktree(s, sess, args[1], force)
	},
}

// pathsToDeleteForKill returns the on-disk paths that killing the whole session
// removes. For in-place layouts the adopted clone root is NEVER included.
func pathsToDeleteForKill(sess *state.Session) []string {
	if sess.Layout == state.LayoutInPlace {
		var paths []string
		for _, w := range sess.Worktrees {
			if w.Name == "main" {
				continue // = Root; never delete
			}
			paths = append(paths, w.Path)
		}
		return paths
	}
	// nested-bare: container artifacts created by eme.
	var paths []string
	for _, w := range sess.Worktrees {
		if w.Name == "main" {
			continue
		}
		paths = append(paths, w.Path)
	}
	return append(paths, filepath.Join(sess.Root, "main"), filepath.Join(sess.Root, ".bare"))
}

func killSession(s *state.State, sess *state.Session) error {
	for _, p := range pathsToDeleteForKill(sess) {
		if err := os.RemoveAll(p); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not remove %s: %v\n", p, err)
		}
	}
	if err := tmux.KillSession(sess.TmuxName); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not kill tmux session %s: %v\n", sess.TmuxName, err)
	}
	s.RemoveSession(sess.ID)
	if err := saveState(s); err != nil {
		return err
	}
	fmt.Printf("Killed session %q\n", sess.DisplayName)
	return nil
}

func killWorktree(s *state.State, sess *state.Session, name string, force bool) error {
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

	if err := git.WorktreeRemove(w.Path, false); err != nil {
		if !force {
			return errors.New(errors.CodeWorktreeDirty,
				fmt.Sprintf("worktree %q has uncommitted or untracked changes.", name),
				"git refused to remove it to avoid data loss.",
				"Commit/stash the work, or run `eme kill <session> "+name+" --force`.")
		}
		// force path: try --force, then double-force for locked worktrees.
		if err := git.WorktreeRemove(w.Path, true); err != nil {
			if _, _, runErr := git.Run(context.Background(), w.Path, "worktree", "remove", "-f", "-f", w.Path); runErr != nil {
				return errors.New(errors.CodeWorktreeLocked,
					fmt.Sprintf("worktree %q is locked and could not be removed even with --force.", name),
					"git refused removal even with double --force; the worktree may be locked or corrupted.",
					"Run `git worktree unlock "+w.Path+"` then retry, or remove it manually.")
			}
		}
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
