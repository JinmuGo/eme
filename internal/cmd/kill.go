package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/alderwork/eme/internal/errors"
	"github.com/alderwork/eme/internal/git"
	"github.com/alderwork/eme/internal/state"
	"github.com/alderwork/eme/internal/tmux"
)

var (
	killDryRun        bool
	killForceUnpushed bool
)

var killCmd = &cobra.Command{
	Use:   "kill <session> [worktree]",
	Short: "Remove a worktree/window or kill an entire session",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		force, _ := cmd.Flags().GetBool("force")
		force = resolveForce(force, killForceUnpushed)
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
			return killSession(s, sess, killForceUnpushed)
		}
		return killWorktree(s, sess, args[1], force)
	},
}

// resolveForce folds --force-unpushed into the general --force gate: the louder override
// (which also discards the only copy of unpushed history) necessarily confirms the
// ordinary removal too, so `eme kill <proj> --force-unpushed` need not also pass --force.
func resolveForce(force, forceUnpushed bool) bool { return force || forceUnpushed }

// pathsToDeleteForKill returns the on-disk paths that killing the whole session
// removes. For in-place layouts the adopted clone root is NEVER included.
func pathsToDeleteForKill(sess *state.Session) []string {
	// A plain (non-git) project created nothing on disk — eme only ran an agent in
	// the user's existing folder — so killing it deletes NOTHING (it just forgets
	// the session and kills the tmux session). Falling through to the nested-bare
	// branch below would target <root>/main and <root>/.bare, wiping a real
	// subdirectory the user happens to have by that name.
	if sess.Layout == state.LayoutPlain {
		return nil
	}
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

// unpushedHistoryGuard refuses to delete a nested-bare project whose .bare holds
// commits reachable from no remote — the only copy of that history dies with the
// folder. Other layouts are exempt: in-place keeps its .git (commits survive in the
// clone root), and plain created no git history at all. The check is best-effort —
// if git can't answer, it fails open rather than wedge the user's ability to delete.
// --force-unpushed bypasses it.
func unpushedHistoryGuard(sess *state.Session, forceUnpushed bool) error {
	if forceUnpushed || sess.Layout != state.LayoutNestedBare {
		return nil
	}
	n, err := git.UnpushedCommitCount(sess.GitDir()) // nested-bare → <root>/.bare
	if err != nil || n == 0 {
		return nil
	}
	return errors.New(errors.CodeUnpushedHistory,
		fmt.Sprintf("%q has %d commit(s) that exist on no remote.", sess.DisplayName, n),
		"Deleting this project removes its .bare repository — the only copy of that history.",
		fmt.Sprintf("Push the branch(es) to a remote first, or run `eme kill %s --force-unpushed` to delete anyway.", sess.DisplayName))
}

func killSession(s *state.State, sess *state.Session, forceUnpushed bool) error {
	if err := unpushedHistoryGuard(sess, forceUnpushed); err != nil {
		return err
	}
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
	killCmd.Flags().BoolVar(&killForceUnpushed, "force-unpushed", false, "also delete a nested-bare project whose history is on no remote (implies --force)")
	killCmd.Flags().BoolVar(&killDryRun, "dry-run", false, "print planned actions without executing")
}
