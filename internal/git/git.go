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

// WorktreeAdd creates a worktree as a sibling of baseDir named name.
func WorktreeAdd(baseDir, name, branch string, newBranch bool) (string, error) {
	target := filepath.Join(filepath.Dir(baseDir), name)
	if err := WorktreeAddAt(baseDir, target, branch, newBranch); err != nil {
		return "", err
	}
	return target, nil
}

// WorktreeAddAt creates a worktree at an arbitrary absolute targetPath, running
// git in repoDir. With newBranch, a new branch (branch, or basename(targetPath)
// if empty) is created with -b.
func WorktreeAddAt(repoDir, targetPath, branch string, newBranch bool) error {
	args := []string{"worktree", "add"}
	if newBranch {
		b := branch
		if b == "" {
			b = filepath.Base(targetPath)
		}
		args = append(args, "-b", b, targetPath)
	} else if branch != "" {
		args = append(args, targetPath, branch)
	} else {
		args = append(args, targetPath)
	}
	if _, _, err := Run(context.Background(), repoDir, args...); err != nil {
		return fmt.Errorf("git worktree add %s: %w", targetPath, err)
	}
	return nil
}

// WorktreeEntry is one row of `git worktree list --porcelain`.
type WorktreeEntry struct {
	Path     string
	Branch   string
	Head     string
	Prunable bool
	Bare     bool
	Detached bool
}

// WorktreeListPorcelain returns structured worktree entries for the repo at dir.
func WorktreeListPorcelain(dir string) ([]WorktreeEntry, error) {
	out, _, err := Run(context.Background(), dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var entries []WorktreeEntry
	var cur *WorktreeEntry
	flush := func() {
		if cur != nil {
			entries = append(entries, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur = &WorktreeEntry{Path: strings.TrimPrefix(line, "worktree ")}
		case cur == nil:
			// ignore
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "bare":
			cur.Bare = true
		case line == "detached":
			cur.Detached = true
		case line == "prunable" || strings.HasPrefix(line, "prunable "):
			cur.Prunable = true
		}
	}
	flush()
	return entries, nil
}

// BranchExists reports whether refs/heads/branch exists in the repo at repoDir.
func BranchExists(repoDir, branch string) bool {
	_, _, err := Run(context.Background(), repoDir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// WorktreeRepair fixes the administrative links of worktreePath after a move.
func WorktreeRepair(gitDir, worktreePath string) error {
	_, _, err := Run(context.Background(), gitDir, "worktree", "repair", worktreePath)
	return err
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
