package git

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jinmu/eme/internal/runner"
)

func TestWorktreeAdd_NewBranch(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("git", []string{"-C", "/tmp/foo/main", "worktree", "add", "-b", "feature", "/tmp/foo/feature"}, "", "", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()

	path, err := WorktreeAdd("/tmp/foo/main", "feature", "", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/tmp/foo/feature" {
		t.Errorf("expected /tmp/foo/feature, got %q", path)
	}
}

func TestWorktreeAddAt_AbsoluteTarget(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("git", []string{"-C", "/repo", "worktree", "add", "-b", "feat", "/repo.worktrees/feat"}, "", "", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()

	if err := WorktreeAddAt("/repo", "/repo.worktrees/feat", "", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.Join(append([]string{mock.Calls[0].Name}, mock.Calls[0].Args...), " ")
	want := "git -C /repo worktree add -b feat /repo.worktrees/feat"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestWorktreeListPorcelain_ParsesPrunable(t *testing.T) {
	mock := runner.NewMock()
	out := "worktree /repo\nHEAD a1\nbranch refs/heads/main\n\n" +
		"worktree /repo.worktrees/dead\nHEAD b2\nbranch refs/heads/dead\nprunable gitdir file points to non-existent location\n\n"
	mock.Set("git", []string{"-C", "/repo", "worktree", "list", "--porcelain"}, out, "", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()

	entries, err := WorktreeListPorcelain("/repo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].Path != "/repo" || entries[0].Branch != "main" || entries[0].Prunable {
		t.Errorf("entry0 = %+v", entries[0])
	}
	if !entries[1].Prunable {
		t.Errorf("entry1 should be prunable: %+v", entries[1])
	}
}

func TestBranchExists(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("git", []string{"-C", "/repo", "show-ref", "--verify", "--quiet", "refs/heads/main"}, "", "", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()
	if !BranchExists("/repo", "main") {
		t.Errorf("expected main to exist")
	}
}

func TestInitBare(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("git", []string{"init", "--bare", "/tmp/foo/.bare"}, "", "", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()

	if err := InitBare("/tmp/foo/.bare"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.Calls))
	}
	got := strings.Join(append([]string{mock.Calls[0].Name}, mock.Calls[0].Args...), " ")
	want := "git init --bare /tmp/foo/.bare"
	if got != want {
		t.Errorf("command mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestRun_PrependsWorkingDir(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("git", []string{"-C", "/tmp/foo", "status"}, "", "", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()

	_, _, err := Run(context.Background(), "/tmp/foo", "status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.Calls))
	}
	got := strings.Join(append([]string{mock.Calls[0].Name}, mock.Calls[0].Args...), " ")
	want := "git -C /tmp/foo status"
	if got != want {
		t.Errorf("command mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestDiffStat_SumsNumstat(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("git", []string{"-C", "/wt", "diff", "HEAD", "--numstat"},
		"12\t3\tmain.go\n0\t5\tx.go\n-\t-\tbin.png\n", "", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()

	added, deleted, ok := DiffStat("/wt")
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if added != 12 || deleted != 8 {
		t.Errorf("added,deleted = %d,%d, want 12,8", added, deleted)
	}
}

func TestDiffStat_ErrorReturnsNotOK(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("git", []string{"-C", "/wt", "diff", "HEAD", "--numstat"}, "", "fatal", errFake)
	Runner = mock
	defer func() { Runner = runner.Default }()

	if _, _, ok := DiffStat("/wt"); ok {
		t.Error("ok = true, want false on git error")
	}
}

var errFake = fmt.Errorf("boom")
