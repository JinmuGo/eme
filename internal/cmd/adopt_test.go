package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExcludeFromParentRepo_AppendsWhenParentIsRepo(t *testing.T) {
	base := t.TempDir()
	// parent is a git work tree (has .git/info/).
	infoDir := filepath.Join(base, ".git", "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	worktreeDir := filepath.Join(base, "myapp.worktrees")

	if err := excludeFromParentRepo(worktreeDir); err != nil {
		t.Fatalf("err: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(infoDir, "exclude"))
	if err != nil {
		t.Fatalf("read exclude: %v", err)
	}
	if !strings.Contains(string(data), "myapp.worktrees/") {
		t.Errorf("exclude missing entry, got: %q", data)
	}
}

func TestExcludeFromParentRepo_NoopWhenParentNotRepo(t *testing.T) {
	base := t.TempDir()
	worktreeDir := filepath.Join(base, "myapp.worktrees")
	if err := excludeFromParentRepo(worktreeDir); err != nil {
		t.Fatalf("err: %v", err)
	}
	// No .git anywhere; nothing created, no error.
}

func TestExcludeFromParentRepo_Idempotent(t *testing.T) {
	base := t.TempDir()
	// parent is a git work tree (has .git/info/).
	infoDir := filepath.Join(base, ".git", "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	worktreeDir := filepath.Join(base, "myapp.worktrees")

	// Call twice to verify idempotency
	if err := excludeFromParentRepo(worktreeDir); err != nil {
		t.Fatalf("first call err: %v", err)
	}
	if err := excludeFromParentRepo(worktreeDir); err != nil {
		t.Fatalf("second call err: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(infoDir, "exclude"))
	if err != nil {
		t.Fatalf("read exclude: %v", err)
	}
	count := strings.Count(string(data), "myapp.worktrees/")
	if count != 1 {
		t.Errorf("entry should appear exactly once, got %d occurrences in:\n%q", count, data)
	}
}
