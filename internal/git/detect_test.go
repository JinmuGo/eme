// internal/git/detect_test.go
package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alderwork/eme/internal/runner"
)

// TestClassify_RealGit_BareGreenfieldNormal exercises Classify against REAL git
// for the three "not inside a work tree" outcomes. A standalone bare repo is the
// regression case: real `git rev-parse --is-inside-work-tree` returns "false"
// with exit 0 (no error), so a mock-only suite never caught the misclassification.
func TestClassify_RealGit_BareGreenfieldNormal(t *testing.T) {
	if testing.Short() {
		t.Skip("uses real git")
	}
	Runner = runner.Default // be explicit; other tests swap and restore Runner

	base := t.TempDir()

	bare := filepath.Join(base, "barerepo.git")
	if _, _, err := runner.Default.Run(context.Background(), "git", "init", "--bare", bare); err != nil {
		t.Fatalf("git init --bare: %v", err)
	}
	if c, err := Classify(bare); err != nil || c.Kind != KindBareRepo {
		t.Errorf("bare repo: Kind = %v, err = %v, want KindBareRepo", c.Kind, err)
	}

	green := filepath.Join(base, "empty")
	if err := os.MkdirAll(green, 0o755); err != nil {
		t.Fatal(err)
	}
	if c, err := Classify(green); err != nil || c.Kind != KindGreenfield {
		t.Errorf("greenfield: Kind = %v, err = %v, want KindGreenfield", c.Kind, err)
	}

	normal := filepath.Join(base, "normal")
	if _, _, err := runner.Default.Run(context.Background(), "git", "init", normal); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if c, err := Classify(normal); err != nil || c.Kind != KindNormalRoot {
		t.Errorf("normal root: Kind = %v, err = %v, want KindNormalRoot", c.Kind, err)
	}
}

// setProbe wires canned rev-parse answers for a dir. Keys must match the
// flags Classify issues (each flag queried individually).
func TestClassify_NormalRoot(t *testing.T) {
	dir := t.TempDir() // real dir so EvalSymlinks works; canonical form below
	canon, _ := filepath.EvalSymlinks(dir)

	m := runner.NewMock()
	m.Set("git", []string{"-C", canon, "rev-parse", "--is-inside-work-tree"}, "true\n", "", nil)
	m.Set("git", []string{"-C", canon, "rev-parse", "--path-format=absolute", "--show-toplevel"}, canon+"\n", "", nil)
	m.Set("git", []string{"-C", canon, "rev-parse", "--path-format=absolute", "--git-dir"}, canon+"/.git\n", "", nil)
	m.Set("git", []string{"-C", canon, "rev-parse", "--path-format=absolute", "--git-common-dir"}, canon+"/.git\n", "", nil)
	m.Set("git", []string{"-C", canon, "rev-parse", "--show-superproject-working-tree"}, "\n", "", nil)
	Runner = m
	defer func() { Runner = runner.Default }()

	c, err := Classify(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Kind != KindNormalRoot {
		t.Errorf("Kind = %v, want KindNormalRoot", c.Kind)
	}
	if c.TopLevel != canon {
		t.Errorf("TopLevel = %q, want %q", c.TopLevel, canon)
	}
}

func TestClassify_LinkedWorktreeResolvesMain(t *testing.T) {
	dir := t.TempDir()
	canon, _ := filepath.EvalSymlinks(dir)
	main := filepath.Dir(canon) + "/myapp"

	m := runner.NewMock()
	m.Set("git", []string{"-C", canon, "rev-parse", "--is-inside-work-tree"}, "true\n", "", nil)
	m.Set("git", []string{"-C", canon, "rev-parse", "--path-format=absolute", "--show-toplevel"}, canon+"\n", "", nil)
	m.Set("git", []string{"-C", canon, "rev-parse", "--path-format=absolute", "--git-dir"}, main+"/.git/worktrees/feat\n", "", nil)
	m.Set("git", []string{"-C", canon, "rev-parse", "--path-format=absolute", "--git-common-dir"}, main+"/.git\n", "", nil)
	m.Set("git", []string{"-C", canon, "rev-parse", "--show-superproject-working-tree"}, "\n", "", nil)
	m.Set("git", []string{"-C", canon, "worktree", "list", "--porcelain"}, "worktree "+main+"\nHEAD abc\nbranch refs/heads/main\n\nworktree "+canon+"\nHEAD def\nbranch refs/heads/feat\n", "", nil)
	Runner = m
	defer func() { Runner = runner.Default }()

	c, err := Classify(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Kind != KindLinkedWorktree {
		t.Errorf("Kind = %v, want KindLinkedWorktree", c.Kind)
	}
	if c.MainPath != main {
		t.Errorf("MainPath = %q, want %q", c.MainPath, main)
	}
}

func TestClassify_SubmoduleRefused(t *testing.T) {
	dir := t.TempDir()
	canon, _ := filepath.EvalSymlinks(dir)
	super := filepath.Dir(canon) + "/superproj"

	m := runner.NewMock()
	m.Set("git", []string{"-C", canon, "rev-parse", "--is-inside-work-tree"}, "true\n", "", nil)
	m.Set("git", []string{"-C", canon, "rev-parse", "--path-format=absolute", "--show-toplevel"}, canon+"\n", "", nil)
	m.Set("git", []string{"-C", canon, "rev-parse", "--path-format=absolute", "--git-dir"}, super+"/.git/modules/sub\n", "", nil)
	m.Set("git", []string{"-C", canon, "rev-parse", "--path-format=absolute", "--git-common-dir"}, super+"/.git/modules/sub\n", "", nil)
	m.Set("git", []string{"-C", canon, "rev-parse", "--show-superproject-working-tree"}, super+"\n", "", nil)
	Runner = m
	defer func() { Runner = runner.Default }()

	c, err := Classify(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Kind != KindSubmodule {
		t.Errorf("Kind = %v, want KindSubmodule", c.Kind)
	}
}

func TestClassify_NestedBareMarkerFirst(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".bare"), 0o755); err != nil {
		t.Fatal(err)
	}
	// No rev-parse should be needed; mock left empty to prove marker-first.
	m := runner.NewMock()
	Runner = m
	defer func() { Runner = runner.Default }()

	c, err := Classify(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Kind != KindNestedBare {
		t.Errorf("Kind = %v, want KindNestedBare", c.Kind)
	}
}
