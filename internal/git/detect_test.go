// internal/git/detect_test.go
package git

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jinmu/eme/internal/runner"
)

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
