// Package git wraps git operations through a runner for testability.
package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jinmu/eme/internal/runner"
)

// Runner is the command runner used by this package. Tests can replace it.
var Runner runner.Runner = runner.Default

// IsRepo reports whether dir is inside a git worktree.
func IsRepo(dir string) bool {
	_, _, err := Run(context.Background(), dir, "rev-parse", "--is-inside-work-tree")
	return err == nil
}

// HasGitDir reports whether dir already contains a .git directory or file.
func HasGitDir(dir string) bool {
	gitPath := filepath.Join(dir, ".git")
	_, err := os.Stat(gitPath)
	return err == nil
}

// TopLevel returns the absolute top-level of the git worktree containing dir.
func TopLevel(dir string) (string, error) {
	out, _, err := Run(context.Background(), dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// InitBare initializes a bare repository at dir.
func InitBare(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	_, _, err := Runner.Run(context.Background(), "git", "init", "--bare", dir)
	return err
}

// SetDefaultBranch sets the bare repo's HEAD to branch.
func SetDefaultBranch(bareDir, branch string) error {
	_, _, err := Run(context.Background(), bareDir, "symbolic-ref", "HEAD", "refs/heads/"+branch)
	return err
}

// CreateEmptyCommit creates an empty commit on branch in the worktree dir.
func CreateEmptyCommit(worktreeDir, branch, message string) error {
	_, _, err := Run(context.Background(), worktreeDir, "commit", "--allow-empty", "-m", message)
	return err
}

// WorktreeAdd creates a new worktree relative to a sibling worktree.
// baseDir: absolute path to an existing worktree (e.g. <project>/main).
// name: directory name for the new worktree (sibling of baseDir).
// branch: branch name; if empty and newBranch is true, name is used as branch.
// newBranch: whether to create a new branch with -b.
func WorktreeAdd(baseDir, name, branch string, newBranch bool) (string, error) {
	target := filepath.Join(filepath.Dir(baseDir), name)
	relPath := "../" + name
	args := []string{"worktree", "add"}

	if newBranch {
		b := branch
		if b == "" {
			b = name
		}
		args = append(args, "-b", b, relPath)
	} else if branch != "" {
		args = append(args, relPath, branch)
	} else {
		args = append(args, relPath)
	}

	_, _, err := Run(context.Background(), baseDir, args...)
	if err != nil {
		return "", fmt.Errorf("git worktree add %s: %w", name, err)
	}
	return target, nil
}

// WorktreeRemove removes a worktree by absolute path.
func WorktreeRemove(path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)

	_, _, err := Run(context.Background(), path, args...)
	return err
}

// WorktreeList returns the absolute paths of all linked worktrees for a repo.
// The bare repo dir is the best place to run this.
func WorktreeList(dir string) ([]string, error) {
	out, _, err := Run(context.Background(), dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			paths = append(paths, strings.TrimPrefix(line, "worktree "))
		}
	}
	return paths, nil
}

// CurrentBranch returns the current branch of a worktree, or "" if detached.
func CurrentBranch(dir string) (string, error) {
	out, _, err := Run(context.Background(), dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Run executes a git command in dir with args using the configured Runner.
func Run(ctx context.Context, dir string, args ...string) (string, string, error) {
	allArgs := append([]string{"-C", dir}, args...)
	return Runner.Run(ctx, "git", allArgs...)
}

// Version returns the installed git version string.
func Version() (string, error) {
	out, _, err := Runner.Run(context.Background(), "git", "--version")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
