package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alderwork/eme/internal/config"
	"github.com/alderwork/eme/internal/git"
	"github.com/alderwork/eme/internal/runner"
)

func TestRepoNameFromSpec(t *testing.T) {
	cases := map[string]string{
		"alderwork/eme":                        "eme",
		"eme":                                "eme",
		"https://github.com/alderwork/eme":     "eme",
		"https://github.com/alderwork/eme.git": "eme",
		"git@github.com:alderwork/eme.git":     "eme",
	}
	for spec, want := range cases {
		if got := repoNameFromSpec(spec); got != want {
			t.Errorf("repoNameFromSpec(%q) = %q, want %q", spec, got, want)
		}
	}
}

func TestResolveCloneDirPrecedence(t *testing.T) {
	prevCfg := cfg
	prevFlag := cloneDirFlag
	t.Cleanup(func() { cfg = prevCfg; cloneDirFlag = prevFlag })

	home, _ := os.UserHomeDir()

	// config value wins over standard roots; ~ expands.
	cfg = config.Default()
	cfg.Clone.Dir = "~/Programming/new"
	cloneDirFlag = ""
	t.Setenv("EME_CLONE_DIR", "")
	if got, _ := resolveCloneDir(); got != filepath.Join(home, "Programming/new") {
		t.Errorf("config dir: got %q", got)
	}

	// env wins over config.
	t.Setenv("EME_CLONE_DIR", "/tmp/envdir")
	if got, _ := resolveCloneDir(); got != "/tmp/envdir" {
		t.Errorf("env dir: got %q", got)
	}

	// flag wins over env.
	cloneDirFlag = "/tmp/flagdir"
	if got, _ := resolveCloneDir(); got != "/tmp/flagdir" {
		t.Errorf("flag dir: got %q", got)
	}
}

func TestCloneBareLayoutSequenceAndCleanup(t *testing.T) {
	dest := t.TempDir()
	bare := filepath.Join(dest, ".bare")
	mainWt := filepath.Join(dest, "main")

	gmock := runner.NewMock()
	gmock.Set("git", []string{"-C", bare, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"}, "", "", nil)
	gmock.Set("git", []string{"-C", bare, "fetch", "origin"}, "", "", nil)
	gmock.Set("git", []string{"-C", bare, "symbolic-ref", "--short", "HEAD"}, "main\n", "", nil)
	// empty-repo guard: default branch exists in the bare clone.
	gmock.Set("git", []string{"-C", bare, "show-ref", "--verify", "--quiet", "refs/heads/main"}, "", "", nil)
	gmock.Set("git", []string{"-C", bare, "worktree", "add", mainWt, "main"}, "", "", nil)
	gmock.Set("git", []string{"-C", mainWt, "branch", "--set-upstream-to=origin/main", "main"}, "", "", nil)
	// prune: clone --bare left local heads main+develop; develop must be deleted.
	gmock.Set("git", []string{"-C", bare, "for-each-ref", "--format=%(refname:short)", "refs/heads/"}, "main\ndevelop\n", "", nil)
	gmock.Set("git", []string{"-C", bare, "branch", "-D", "develop"}, "", "", nil)
	prevG := git.Runner
	git.Runner = gmock
	t.Cleanup(func() { git.Runner = prevG })

	// Simulate gh's bare clone by creating the .bare dir the clone would produce.
	branch, err := cloneBareLayoutWith(context.Background(), dest, func(_ context.Context) error {
		return os.MkdirAll(bare, 0o755)
	})
	if err != nil {
		t.Fatalf("cloneBareLayout: %v", err)
	}
	if branch != "main" {
		t.Errorf("branch = %q, want main", branch)
	}
}
