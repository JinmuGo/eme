package cmd

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// mkdirs creates each given path (relative to base) as a directory.
func mkdirs(t *testing.T, base string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(filepath.Join(base, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func relset(t *testing.T, base string, folders []string) map[string]bool {
	t.Helper()
	out := make(map[string]bool)
	for _, f := range folders {
		rel, err := filepath.Rel(base, f)
		if err != nil {
			t.Fatalf("rel %q: %v", f, err)
		}
		out[rel] = true
	}
	return out
}

func TestCollectFolders_DepthLimit(t *testing.T) {
	base := t.TempDir()
	mkdirs(t, base, "a/b/c/d")

	got := relset(t, base, collectFolders([]string{base}, 3))

	for _, want := range []string{"a", "a/b", "a/b/c"} {
		if !got[want] {
			t.Errorf("expected %q within depth 3, missing", want)
		}
	}
	if got["a/b/c/d"] {
		t.Errorf("depth-4 %q should be excluded with maxDepth=3", "a/b/c/d")
	}
}

func TestCollectFolders_ShallowerDepth(t *testing.T) {
	base := t.TempDir()
	mkdirs(t, base, "a/b/c")

	got := relset(t, base, collectFolders([]string{base}, 2))
	if !got["a"] || !got["a/b"] {
		t.Errorf("expected a and a/b at depth 2, got %v", got)
	}
	if got["a/b/c"] {
		t.Errorf("depth-3 a/b/c should be excluded with maxDepth=2")
	}
}

func TestCollectFolders_SkipsHiddenAndDenylisted(t *testing.T) {
	base := t.TempDir()
	mkdirs(t, base,
		".hidden/inner",
		"node_modules/pkg",
		"Library/foo",
		"keep",
	)

	got := relset(t, base, collectFolders([]string{base}, 3))
	if !got["keep"] {
		t.Errorf("expected 'keep' to be listed")
	}
	for _, bad := range []string{".hidden", ".hidden/inner", "node_modules", "node_modules/pkg", "Library", "Library/foo"} {
		if got[bad] {
			t.Errorf("hidden/denylisted %q should be excluded", bad)
		}
	}
}

func TestCollectFolders_StopsAtProjectBoundary(t *testing.T) {
	base := t.TempDir()
	mkdirs(t, base,
		"repo/internal/deep",
		"barelike/.bare",
		"barelike/main",
		"normal/sub",
	)
	// Make repo a git repo (a .git file, like a worktree, also counts).
	if err := os.MkdirAll(filepath.Join(base, "repo", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := relset(t, base, collectFolders([]string{base}, 5))

	// Boundaries themselves are listed...
	if !got["repo"] {
		t.Errorf("repo (git repo) should be listed")
	}
	if !got["barelike"] {
		t.Errorf("barelike (.bare layout) should be listed")
	}
	// ...but we do not descend into them.
	for _, bad := range []string{"repo/internal", "repo/internal/deep", "barelike/main"} {
		if got[bad] {
			t.Errorf("should not descend into project boundary: %q", bad)
		}
	}
	// Non-boundary descent still works.
	if !got["normal"] || !got["normal/sub"] {
		t.Errorf("normal/sub should be listed, got %v", got)
	}
}

func TestCollectFolders_DedupesAcrossRoots(t *testing.T) {
	base := t.TempDir()
	mkdirs(t, base, "shared")

	// Same root twice plus a child root that overlaps.
	folders := collectFolders([]string{base, base, filepath.Join(base, "shared")}, 3)
	if !slices.IsSorted(folders) {
		t.Errorf("folders should be sorted: %v", folders)
	}
	seen := map[string]bool{}
	for _, f := range folders {
		if seen[f] {
			t.Errorf("duplicate folder %q", f)
		}
		seen[f] = true
	}
}
