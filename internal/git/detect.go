package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// Kind describes how eme should treat a picked folder.
type Kind int

const (
	KindGreenfield     Kind = iota // not a repo, empty/no-git
	KindNormalRoot                 // normal repo root → adopt in-place
	KindNestedBare                 // <dir>/.bare present → existing eme project
	KindLinkedWorktree             // a linked worktree → resolve to MainPath
	KindSubmodule                  // submodule working tree → refuse
	KindBareRepo                   // standalone bare repo → out of scope
	KindSubdirectory               // a subdir of a repo → resolve to TopLevel
	KindBrokenGit                  // present-but-broken .git pointer → error
)

// Classification is the result of Classify.
type Classification struct {
	Kind     Kind
	TopLevel string // canonical, for NormalRoot/Subdirectory/LinkedWorktree
	// MainPath is the canonical path of the main worktree, populated for
	// KindLinkedWorktree. An empty MainPath means the main worktree could not
	// be resolved (e.g. the porcelain output was missing); callers must guard
	// against it before using this value.
	MainPath string
}

// strippedGitEnvVars are removed from the detection environment so an inherited
// GIT_DIR/GIT_WORK_TREE does not make git report the ambient repo instead of dir.
var strippedGitEnvVars = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
}

// detectEnv returns the process environment with git-context vars removed.
func detectEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		drop := false
		for _, k := range strippedGitEnvVars {
			if strings.HasPrefix(kv, k+"=") {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}

// probe runs a sanitized git command in dir, returning trimmed stdout and the error.
func probe(dir string, args ...string) (string, error) {
	all := append([]string{"-C", dir}, args...)
	out, _, err := Runner.RunEnv(context.Background(), detectEnv(), "git", all...)
	return strings.TrimSpace(out), err
}

// Classify determines how eme should treat the picked folder dir.
// For KindLinkedWorktree, Classification.MainPath may be empty if the main
// worktree could not be resolved; callers must guard against an empty MainPath.
func Classify(dir string) (Classification, error) {
	canon, err := filepath.EvalSymlinks(dir)
	if err != nil {
		canon = dir
	}

	// Filesystem markers BEFORE rev-parse: a nested-bare container reports the
	// same "not a git repository" as an empty dir.
	if fi, err := os.Stat(filepath.Join(canon, ".bare")); err == nil && fi.IsDir() {
		return Classification{Kind: KindNestedBare, TopLevel: canon}, nil
	}

	if _, err := probe(canon, "rev-parse", "--is-inside-work-tree"); err != nil {
		// Not inside a work tree: bare repo, greenfield, or broken pointer.
		if bare, berr := probe(canon, "rev-parse", "--is-bare-repository"); berr == nil && bare == "true" {
			return Classification{Kind: KindBareRepo, TopLevel: canon}, nil
		}
		if gitPath := filepath.Join(canon, ".git"); fileExists(gitPath) {
			return Classification{Kind: KindBrokenGit, TopLevel: canon}, nil
		}
		return Classification{Kind: KindGreenfield, TopLevel: canon}, nil
	}

	top, _ := probe(canon, "rev-parse", "--path-format=absolute", "--show-toplevel")
	gd, _ := probe(canon, "rev-parse", "--path-format=absolute", "--git-dir")
	cd, _ := probe(canon, "rev-parse", "--path-format=absolute", "--git-common-dir")
	super, _ := probe(canon, "rev-parse", "--show-superproject-working-tree")

	top = canonical(top)

	if strings.TrimSpace(super) != "" {
		return Classification{Kind: KindSubmodule, TopLevel: top}, nil
	}
	if canonical(gd) != canonical(cd) {
		main := firstWorktree(canon)
		return Classification{Kind: KindLinkedWorktree, TopLevel: top, MainPath: main}, nil
	}
	if top != canon {
		return Classification{Kind: KindSubdirectory, TopLevel: top}, nil
	}
	return Classification{Kind: KindNormalRoot, TopLevel: top}, nil
}

func firstWorktree(dir string) string {
	out, err := probe(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			return canonical(strings.TrimPrefix(line, "worktree "))
		}
	}
	return ""
}

func canonical(p string) string {
	if p == "" {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
