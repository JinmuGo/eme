package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/alderwork/eme/internal/runner"
)

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestUnpushedCommitCount_Integration exercises the real rev-list query against a real
// repo with a remote. The critical case is the tag-only commit: after the branch is
// reset back onto pushed history, a commit kept alive only by a tag must STILL count as
// unpushed — the blind spot the old --branches query had, which would have let
// `eme kill` destroy the only copy of a tagged-but-unpushed commit.
func TestUnpushedCommitCount_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs real git")
	}
	Runner = runner.Default // ensure a real git, not a leaked mock

	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	gitCmd(t, base, "init", "--bare", origin)

	work := filepath.Join(base, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, work, "init", "-b", "main")
	gitCmd(t, work, "config", "user.email", "t@t")
	gitCmd(t, work, "config", "user.name", "t")
	gitCmd(t, work, "remote", "add", "origin", origin)

	commit := func(msg string) {
		if err := os.WriteFile(filepath.Join(work, "f.txt"), []byte(msg), 0o644); err != nil {
			t.Fatal(err)
		}
		gitCmd(t, work, "add", "-A")
		gitCmd(t, work, "commit", "-m", msg)
	}

	// c1 pushed → fully backed up → nothing unpushed.
	commit("c1")
	gitCmd(t, work, "push", "-u", "origin", "main")
	if n, err := UnpushedCommitCount(work); err != nil || n != 0 {
		t.Fatalf("fully pushed: n=%d err=%v, want 0", n, err)
	}

	// c2 committed but not pushed → exactly one commit exists only locally.
	commit("c2")
	if n, err := UnpushedCommitCount(work); err != nil || n != 1 {
		t.Fatalf("one local commit: n=%d err=%v, want 1", n, err)
	}

	// Tag c2, then move main back onto the pushed c1. main now matches origin/main, but
	// c2 survives via the tag — --all must still see it (--branches would report 0).
	gitCmd(t, work, "tag", "keep")
	gitCmd(t, work, "reset", "--hard", "HEAD~1")
	if n, err := UnpushedCommitCount(work); err != nil || n != 1 {
		t.Fatalf("tag-only unpushed commit: n=%d err=%v, want 1 (the --branches blind spot)", n, err)
	}
}
