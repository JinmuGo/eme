package convert

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alderwork/eme/internal/errors"
	"github.com/alderwork/eme/internal/git"
	"github.com/alderwork/eme/internal/runner"
)

func TestConvert_StashNotImplemented(t *testing.T) {
	dir := t.TempDir()
	_, err := Convert(dir, Options{Stash: true})
	if err == nil {
		t.Fatal("expected an error for --stash, got nil")
	}
	if e := errors.As(err); e == nil {
		t.Fatalf("expected *errors.EmeError, got %T: %v", err, err)
	}
	// Must not have mutated disk: no .bare directory created.
	if _, statErr := os.Stat(filepath.Join(dir, ".bare")); statErr == nil {
		t.Error(".bare was created — stash guard did not fire before disk mutations")
	}
	// No temp convert directory created.
	if _, statErr := os.Stat(dir + ".eme-convert"); statErr == nil {
		t.Error(".eme-convert temp was created — stash guard did not fire before disk mutations")
	}
}

func TestCheckPreconditions_RefusesDirtyTree(t *testing.T) {
	m := runner.NewMock()
	m.Set("git", []string{"-C", "/repo", "status", "--porcelain"}, " M file.go\n", "", nil)
	git.Runner = m
	defer func() { git.Runner = runner.Default }()

	err := CheckPreconditions("/repo", Options{})
	if e := errors.As(err); e == nil || e.Code != errors.CodeDirtyTree {
		t.Fatalf("got %v, want dirty_tree", err)
	}
}

func TestCheckPreconditions_RefusesInProgressOp(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	os.WriteFile(filepath.Join(root, ".git", "MERGE_HEAD"), []byte("ref\n"), 0o644)

	m := runner.NewMock()
	m.Set("git", []string{"-C", root, "status", "--porcelain"}, "", "", nil)
	git.Runner = m
	defer func() { git.Runner = runner.Default }()

	err := CheckPreconditions(root, Options{})
	if e := errors.As(err); e == nil || e.Code != errors.CodeInProgressOp {
		t.Fatalf("got %v, want in_progress_op", err)
	}
}

func TestCheckPreconditions_RefusesSubmodules(t *testing.T) {
	m := runner.NewMock()
	m.Set("git", []string{"-C", "/repo", "status", "--porcelain"}, "", "", nil)
	// .gitmodules present → ls-files reports it.
	m.Set("git", []string{"-C", "/repo", "ls-files", "--", ".gitmodules"}, ".gitmodules\n", "", nil)
	git.Runner = m
	defer func() { git.Runner = runner.Default }()

	err := CheckPreconditions("/repo", Options{})
	if e := errors.As(err); e == nil || e.Code != errors.CodeSubmodulesUnsupported {
		t.Fatalf("got %v, want submodules_unsupported", err)
	}
}
