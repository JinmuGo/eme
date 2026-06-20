// Package convert restructures a normal clone into the nested-bare layout
// losslessly (cp -al the gitdir; original stays read-only) with atomic swap.
package convert

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jinmu/eme/internal/errors"
	"github.com/jinmu/eme/internal/git"
)

type Options struct {
	Stash       bool
	NoUntracked bool
}

// CheckPreconditions verifies root can be converted safely.
func CheckPreconditions(root string, opts Options) error {
	out, _, err := git.Run(context.Background(), root, "status", "--porcelain")
	if err != nil {
		return errors.Wrap(errors.CodeCommandFailed, "Failed to read git status.",
			"git status --porcelain failed.", "Run `eme doctor` to verify git.", err)
	}
	if strings.TrimSpace(out) != "" && !opts.Stash {
		return errors.New(errors.CodeDirtyTree,
			"The repository has uncommitted or untracked changes.",
			"Convert needs a clean working tree to swap layouts safely.",
			"Commit or stash your changes, or pass --stash.")
	}
	gitDir := filepath.Join(root, ".git")
	for _, marker := range []string{"MERGE_HEAD", "rebase-merge", "rebase-apply", "CHERRY_PICK_HEAD", "REVERT_HEAD", "BISECT_LOG"} {
		if _, statErr := os.Stat(filepath.Join(gitDir, marker)); statErr == nil {
			return errors.New(errors.CodeInProgressOp,
				"The repository has an in-progress git operation (rebase, merge, cherry-pick, revert, or bisect).",
				"Converting while an operation is mid-flight can corrupt the repository during the layout swap.",
				"Finish or abort the operation (e.g. `git rebase --abort`), then retry.")
		}
	}
	mods, _, _ := git.Run(context.Background(), root, "ls-files", "--", ".gitmodules")
	if strings.TrimSpace(mods) != "" {
		return errors.New(errors.CodeSubmodulesUnsupported,
			"This repository uses submodules.",
			"Converting submodules safely is not supported yet (relative gitdir paths break on the move).",
			"Adopt it in place instead (run `eme new` without --convert).")
	}
	return nil
}

// Convert turns the normal clone at root into the nested-bare layout, returning
// the path of the retained backup. The original is read-only until the final swap.
func Convert(root string, opts Options) (string, error) {
	if opts.Stash {
		return "", errors.New(errors.CodeCommandFailed,
			"Converting with --stash is not implemented yet.",
			"Auto-stashing uncommitted changes during conversion is not available.",
			"Commit or stash your changes manually, then retry without --stash.")
	}
	if err := CheckPreconditions(root, opts); err != nil {
		return "", err
	}
	branch, _ := git.CurrentBranch(root) // may be empty (detached)
	if branch == "" || branch == "HEAD" {
		// fall back to a detached checkout at HEAD; mint accordingly.
		branch = ""
	}

	tmp := root + ".eme-convert"
	backup := root + ".backup-" + nonce()
	if exists(tmp) || exists(backup) {
		return "", errors.New(errors.CodeCommandFailed,
			"A convert temp or backup path already exists.",
			fmt.Sprintf("%s or %s is in the way.", tmp, backup),
			"Remove the stale path and retry.")
	}
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return "", err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }

	// 3. cp -al the gitdir; original stays read-only.
	bare := filepath.Join(tmp, ".bare")
	if err := runCmd("cp", "-al", filepath.Join(root, ".git"), bare); err != nil {
		cleanup()
		return "", wrapConvert("copy gitdir", err)
	}
	// config core.bare via git config (atomic rename → original untouched).
	if _, _, err := git.Run(context.Background(), bare, "config", "core.bare", "true"); err != nil {
		cleanup()
		return "", wrapConvert("set core.bare", err)
	}
	_, _, _ = git.Run(context.Background(), bare, "config", "--unset", "core.worktree")

	// 4. mint main worktree (no checkout), move real files over, reset index.
	mainTmp := filepath.Join(tmp, "main")
	addArgs := []string{"worktree", "add", "--no-checkout", mainTmp}
	if branch != "" {
		addArgs = append(addArgs, branch)
	}
	if _, _, err := git.Run(context.Background(), bare, addArgs...); err != nil {
		cleanup()
		return "", wrapConvert("mint worktree", err)
	}
	if err := moveWorkingTree(root, mainTmp); err != nil {
		cleanup()
		return "", wrapConvert("move working tree", err)
	}
	if _, _, err := git.Run(context.Background(), mainTmp, "reset", "HEAD"); err != nil {
		cleanup()
		return "", wrapConvert("reset index", err)
	}

	// 5. pre-swap verify.
	if out, _, _ := git.Run(context.Background(), mainTmp, "status", "--porcelain"); strings.TrimSpace(out) != "" {
		cleanup()
		return "", wrapConvert("pre-swap verify", fmt.Errorf("worktree not clean"))
	}

	// 6. atomic swap with rollback.
	if err := os.Rename(root, backup); err != nil {
		cleanup()
		return "", wrapConvert("backup original", err)
	}
	if err := os.Rename(tmp, root); err != nil {
		_ = os.Rename(backup, root) // rollback
		return "", wrapConvert("swap", err)
	}

	// 7. repair stale worktree paths (gitfile + bare admin path).
	if err := git.WorktreeRepair(filepath.Join(root, ".bare"), filepath.Join(root, "main")); err != nil {
		return "", restoreBackup(root, backup, "repair", err)
	}

	// 8. post-swap re-verify.
	if out, _, err := git.Run(context.Background(), filepath.Join(root, "main"), "status", "--porcelain"); err != nil || strings.TrimSpace(out) != "" {
		return "", restoreBackup(root, backup, "post-swap verify", fmt.Errorf("status: %s err: %v", out, err))
	}
	return backup, nil
}

func runCmd(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %v\n%s", name, args, err, out)
	}
	return nil
}

func moveWorkingTree(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	var moved []string
	for _, e := range entries {
		if e.Name() == ".git" {
			continue // leave the original's .git in place (read-only source)
		}
		if err := os.Rename(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			for _, n := range moved { // restore src to its original state
				_ = os.Rename(filepath.Join(dst, n), filepath.Join(src, n))
			}
			return err
		}
		moved = append(moved, e.Name())
	}
	return nil
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func nonce() string { return fmt.Sprintf("%d", time.Now().UnixNano()) }

// restoreBackup rolls the swap back after a post-swap failure. If the backup
// cannot be moved back into place, it returns an error that names the backup
// path so the user can recover manually.
func restoreBackup(root, backup, step string, cause error) error {
	_ = os.RemoveAll(root)
	if err := os.Rename(backup, root); err != nil {
		return errors.Wrap(errors.CodeCommandFailed,
			"Convert failed at: "+step+", and automatic rollback also failed.",
			fmt.Sprintf("Your original repository is preserved at %s but could not be moved back to %s automatically.", backup, root),
			"Restore it manually: `mv "+backup+" "+root+"`.", cause)
	}
	return wrapConvert(step, cause)
}

func wrapConvert(step string, err error) error {
	return errors.Wrap(errors.CodeCommandFailed,
		"Convert failed at: "+step+".",
		"The original clone is preserved.",
		"Inspect the error and retry; your repo was not modified.", err)
}
