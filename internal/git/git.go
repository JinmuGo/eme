// Package git wraps git operations through a runner for testability.
package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/JinmuGo/eme/internal/runner"
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
	if _, stderr, err := Run(context.Background(), repoDir, args...); err != nil {
		// Surface git's own message (e.g. "fatal: cannot lock ref ...") instead of
		// only the exit status, so the failure is self-diagnosable. git writes the
		// reason to stderr; fall back to the raw error when it is empty.
		if msg := strings.TrimSpace(stderr); msg != "" {
			return fmt.Errorf("git worktree add %s: %s", targetPath, msg)
		}
		return fmt.Errorf("git worktree add %s: %w", targetPath, err)
	}
	return nil
}

// BranchDFConflict reports a directory/file ref conflict that would stop git from
// creating a branch named `name`, returning the existing branch it collides with.
// git stores each branch as a path under refs/heads/, so `name` cannot be created
// when either:
//   - an existing branch lives under the name/ namespace (forward: "feat" is blocked
//     by "feat/x"), or
//   - an existing branch is an ancestor segment of name (reverse: "feat/x" is blocked
//     by "feat").
//
// Segments are compared case-insensitively when the repo's filesystem is
// case-insensitive (core.ignorecase, e.g. macOS default): there git's loose-ref lock
// stat()s the refs directory, so it would still abort on a case-mismatched name like
// "Feat" when "feat/x" exists — which a case-sensitive match would miss. On a
// case-sensitive filesystem those names are genuinely distinct and allowed by git, so
// the fold is gated off to avoid false positives.
//
// eme pre-checks this so a worktree name reuses git's exact failure as a friendly,
// actionable error before any side effect, rather than a raw "exit status 255".
func BranchDFConflict(repoDir, name string) (string, bool) {
	out, _, err := Run(context.Background(), repoDir, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
	if err != nil {
		return "", false // can't enumerate; git will still surface any real failure
	}
	fold := ignoreCase(repoDir)
	for _, ref := range strings.Split(strings.TrimSpace(out), "\n") {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		// Forward (ref under name/…) or reverse (name under ref/…) — either is a
		// directory/file conflict; report the existing branch that collides.
		if isBranchDescendant(ref, name, fold) || isBranchDescendant(name, ref, fold) {
			return ref, true
		}
	}
	return "", false
}

// isBranchDescendant reports whether child is strictly under parent's ref namespace,
// i.e. every segment of parent is a prefix of child and child has at least one more
// segment. Segments are compared case-insensitively when fold is set.
func isBranchDescendant(child, parent string, fold bool) bool {
	cs := strings.Split(child, "/")
	ps := strings.Split(parent, "/")
	if len(cs) <= len(ps) {
		return false
	}
	for i := range ps {
		if fold {
			if !strings.EqualFold(cs[i], ps[i]) {
				return false
			}
		} else if cs[i] != ps[i] {
			return false
		}
	}
	return true
}

// ignoreCase reports whether the repo treats ref paths case-insensitively
// (core.ignorecase, which git sets from a filesystem probe at init time).
func ignoreCase(repoDir string) bool {
	out, _, err := Run(context.Background(), repoDir, "config", "--get", "core.ignorecase")
	return err == nil && strings.TrimSpace(out) == "true"
}

// RemoteCarriersOf returns the distinct remotes that carry a branch named name (i.e.
// have a ref refs/remotes/<remote>/<name>). git's worktree-add DWIM can materialize a
// local tracking branch only when EXACTLY ONE remote carries it; with several it
// refuses to guess, so callers must disambiguate rather than let git emit a raw fatal.
func RemoteCarriersOf(repoDir, name string) []string {
	out, _, err := Run(context.Background(), repoDir, "for-each-ref", "--format=%(refname)", "refs/remotes/")
	if err != nil {
		return nil
	}
	var remotes []string
	seen := map[string]bool{}
	for _, ref := range strings.Split(strings.TrimSpace(out), "\n") {
		// ref is "refs/remotes/<remote>/<name...>"; split off the remote, match the rest.
		r := strings.TrimPrefix(strings.TrimSpace(ref), "refs/remotes/")
		if i := strings.IndexByte(r, '/'); i >= 0 && r[i+1:] == name {
			if remote := r[:i]; !seen[remote] {
				seen[remote] = true
				remotes = append(remotes, remote)
			}
		}
	}
	return remotes
}

// RemoteBranchExists reports whether any remote carries a branch named name.
func RemoteBranchExists(repoDir, name string) bool {
	return len(RemoteCarriersOf(repoDir, name)) > 0
}

// DeleteBranch force-deletes a local branch. Used to undo a branch eme itself created
// when a later step of worktree creation fails, so a retry does not silently check out
// the half-built leftover.
func DeleteBranch(repoDir, name string) error {
	_, _, err := Run(context.Background(), repoDir, "branch", "-D", name)
	return err
}

// UnpushedCommitCount returns how many commits reachable from ANY local ref are NOT
// reachable from a remote-tracking ref — history that exists ONLY in this repository.
// --all spans branches, tags, stash and HEAD (not just branch tips), so a commit held
// only by a tag or a detached HEAD still counts; --not --remotes subtracts everything a
// remote already has. A repo with no remotes therefore reports all of its history
// (nothing is "pushed"), so the count doubles as a "deleting this discards the only
// copy" signal. gitDir may be a bare repo (e.g. a nested-bare project's .bare).
func UnpushedCommitCount(gitDir string) (int, error) {
	out, _, err := Run(context.Background(), gitDir, "rev-list", "--all", "--not", "--remotes", "--count")
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("parse rev-list count %q: %w", out, err)
	}
	return n, nil
}

// WorktreePrune drops administrative entries for worktrees whose directories are gone,
// so a failed-then-retried creation does not leave a stale registration behind.
func WorktreePrune(repoDir string) error {
	_, _, err := Run(context.Background(), repoDir, "worktree", "prune")
	return err
}

// BranchCheckedOutAt returns the worktree path where branch name is currently checked
// out, if any. A branch can be checked out in at most one worktree, so a match means
// `worktree add` would refuse to check it out again.
func BranchCheckedOutAt(repoDir, name string) (string, bool) {
	entries, err := WorktreeListPorcelain(repoDir)
	if err != nil {
		return "", false
	}
	// A prunable entry is a dead worktree (its directory was removed out from under git),
	// not a live checkout — skip it, so a leftover .worktrees/<name> admin entry never
	// masquerades as holding its branch. createWorktree prunes such entries and reuses the
	// branch; reporting them here would refuse that instead.
	live := func(e WorktreeEntry) bool { return e.Branch != "" && !e.Prunable }

	// Exact match first — the common case, and the only correct one on a
	// case-sensitive filesystem where "Feat" and "feat" are distinct branches.
	for _, e := range entries {
		if live(e) && e.Branch == name {
			return e.Path, true
		}
	}
	// On a case-insensitive filesystem (core.ignorecase, e.g. macOS default) git resolves
	// refs case-insensitively, so a name differing only in case still collides. BranchExists
	// is an FS-backed show-ref and already sees it; fold here too so the two checks agree and
	// createWorktree switches/refuses cleanly instead of falling through to a raw git fatal
	// ("'<branch>' is already used by worktree at …"). Only consulted when no exact match,
	// so the extra config read costs nothing on the hot path.
	if ignoreCase(repoDir) {
		for _, e := range entries {
			if live(e) && strings.EqualFold(e.Branch, name) {
				return e.Path, true
			}
		}
	}
	return "", false
}

// BranchesUnderNamespace returns existing branches that live under name/ (the forward
// directory/file-conflict candidates — e.g. the feat/* branches for name "feat"),
// folding case on a case-insensitive filesystem so the suggestions match what actually
// blocks the name.
func BranchesUnderNamespace(repoDir, name string) []string {
	out, _, err := Run(context.Background(), repoDir, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
	if err != nil {
		return nil
	}
	fold := ignoreCase(repoDir)
	var branches []string
	for _, ref := range strings.Split(strings.TrimSpace(out), "\n") {
		if ref = strings.TrimSpace(ref); ref != "" && isBranchDescendant(ref, name, fold) {
			branches = append(branches, ref)
		}
	}
	return branches
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

// SetFetchRefspec points a bare clone's origin fetch refspec at refs/remotes so
// future fetches populate remote-tracking refs instead of overwriting the local
// refs/heads/* that back worktrees.
func SetFetchRefspec(bareDir string) error {
	_, _, err := Run(context.Background(), bareDir, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	return err
}

// Fetch runs `git fetch origin` in dir.
func Fetch(dir string) error {
	_, _, err := Run(context.Background(), dir, "fetch", "origin")
	return err
}

// DefaultBranch returns the branch HEAD points at in a (bare) repo, e.g. "main".
func DefaultBranch(dir string) (string, error) {
	out, _, err := Run(context.Background(), dir, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// SetUpstream sets branch's upstream to upstream (e.g. "origin/main") in worktreeDir.
func SetUpstream(worktreeDir, branch, upstream string) error {
	_, _, err := Run(context.Background(), worktreeDir, "branch", "--set-upstream-to="+upstream, branch)
	return err
}

// ListLocalBranches returns the local branch short names in dir (a bare repo's
// refs/heads/*). Returns an empty slice for a repo with no commits/branches.
func ListLocalBranches(dir string) ([]string, error) {
	out, _, err := Run(context.Background(), dir, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if b := strings.TrimSpace(line); b != "" {
			names = append(names, b)
		}
	}
	return names, nil
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

// DiffStat returns the added and deleted line counts of uncommitted changes in
// dir (working tree vs HEAD) via `git -C dir diff HEAD --numstat`. ok is false
// on any error; callers treat that as "no stat" and render nothing. Binary
// files (numstat "-") contribute zero.
func DiffStat(dir string) (added, deleted int, ok bool) {
	out, _, err := Run(context.Background(), dir, "diff", "HEAD", "--numstat")
	if err != nil {
		return 0, 0, false
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		a, errA := strconv.Atoi(fields[0])
		d, errD := strconv.Atoi(fields[1])
		if errA != nil || errD != nil {
			continue // binary "-" rows
		}
		added += a
		deleted += d
	}
	return added, deleted, true
}
