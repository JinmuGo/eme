// internal/convert/convert_integration_test.go
package convert

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestConvert_PreservesStashAndStructure(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs real git")
	}
	base := t.TempDir()
	repo := filepath.Join(base, "myapp")
	gitCmd(t, base, "init", repo)
	gitCmd(t, repo, "config", "user.email", "t@t")
	gitCmd(t, repo, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repo, "add", "f.txt")
	gitCmd(t, repo, "commit", "-m", "init")
	// create a stash to prove it survives.
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repo, "stash")

	backup, err := Convert(repo, Options{})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	// Layout: .bare + main exist.
	if fi, err := os.Stat(filepath.Join(repo, ".bare")); err != nil || !fi.IsDir() {
		t.Errorf(".bare missing")
	}
	mainDir := filepath.Join(repo, "main")
	if fi, err := os.Stat(mainDir); err != nil || !fi.IsDir() {
		t.Errorf("main missing")
	}
	// Worktree functional + stash preserved.
	status := gitCmd(t, mainDir, "status", "--porcelain")
	if strings.TrimSpace(status) != "" {
		t.Errorf("main not clean: %q", status)
	}
	stashes := gitCmd(t, mainDir, "stash", "list")
	if !strings.Contains(stashes, "stash@{0}") {
		t.Errorf("stash lost: %q", stashes)
	}
	// Backup retained and intact.
	if _, err := os.Stat(backup); err != nil {
		t.Errorf("backup missing: %v", err)
	}
}
