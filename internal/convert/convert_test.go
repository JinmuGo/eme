package convert

import (
	"testing"

	"github.com/jinmu/eme/internal/errors"
	"github.com/jinmu/eme/internal/git"
	"github.com/jinmu/eme/internal/runner"
)

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
