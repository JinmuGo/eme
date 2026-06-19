package git

import (
	"context"
	"strings"
	"testing"

	"github.com/jinmu/eme/internal/runner"
)

func TestWorktreeAdd_NewBranch(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("git", []string{"-C", "/tmp/foo/main", "worktree", "add", "-b", "feature", "../feature"}, "", "", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()

	path, err := WorktreeAdd("/tmp/foo/main", "feature", "", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/tmp/foo/feature" {
		t.Errorf("expected /tmp/foo/feature, got %q", path)
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.Calls))
	}
	got := strings.Join(append([]string{mock.Calls[0].Name}, mock.Calls[0].Args...), " ")
	want := "git -C /tmp/foo/main worktree add -b feature ../feature"
	if got != want {
		t.Errorf("command mismatch\n got: %s\nwant: %s", got, want)
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
