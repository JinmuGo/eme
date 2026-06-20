package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jinmu/eme/internal/errors"
	"github.com/jinmu/eme/internal/git"
	"github.com/jinmu/eme/internal/state"
)

func TestRouteByClassification(t *testing.T) {
	cases := []struct {
		kind    git.Kind
		wantErr string // expected error code
	}{
		{git.KindSubmodule, errors.CodeSubmoduleRepo},
		{git.KindBareRepo, errors.CodeBareRepo},
		{git.KindBrokenGit, errors.CodeBrokenGit},
	}
	for _, tc := range cases {
		err := routeByClassification(git.Classification{Kind: tc.kind, TopLevel: "/x"}, false)
		if e := errors.As(err); e == nil || e.Code != tc.wantErr {
			t.Errorf("kind %v: got %v, want code %s", tc.kind, err, tc.wantErr)
		}
	}
}

func TestCreateProject_EmptyFolderRejected(t *testing.T) {
	// Regression: a cancelled folder picker used to reach createProject with an
	// empty path. filepath.Abs("") resolves to the cwd, so createProject would
	// classify the current directory and adopt/switch to it — the "session jump
	// on Ctrl+C" bug. The guard must reject empty/whitespace before any side
	// effect (no git, tmux, or state access).
	for _, in := range []string{"", "   ", "\t"} {
		err := createProject(in)
		e := errors.As(err)
		if e == nil || e.Code != errors.CodeInvalidFolder {
			t.Errorf("createProject(%q) = %v, want code %s", in, err, errors.CodeInvalidFolder)
		}
	}
}

func TestWorktreeTargetPath(t *testing.T) {
	nested := &state.Session{Root: "/p/app", Layout: state.LayoutNestedBare}
	if got := worktreeTargetPath(nested, "feat"); got != "/p/app/feat" {
		t.Errorf("nested target = %q", got)
	}
	inplace := &state.Session{Root: "/p/app", Layout: state.LayoutInPlace}
	if got := worktreeTargetPath(inplace, "feat"); got != "/p/app.worktrees/feat" {
		t.Errorf("in-place target = %q", got)
	}
}

func TestConvertFlagRegistered(t *testing.T) {
	if newCmd.Flags().Lookup("convert") == nil {
		t.Errorf("--convert flag not registered on newCmd")
	}
}

func TestScanFolders_DeduplicatesAndSkipsHidden(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create a visible dir and a hidden dir.
	visible := filepath.Join(home, "Projects")
	if err := os.MkdirAll(visible, 0o755); err != nil {
		t.Fatal(err)
	}
	hidden := filepath.Join(home, ".hidden")
	if err := os.MkdirAll(hidden, 0o755); err != nil {
		t.Fatal(err)
	}

	folders, err := scanFolders()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	seen := make(map[string]bool)
	for _, f := range folders {
		if seen[f] {
			t.Errorf("duplicate folder %q", f)
		}
		seen[f] = true
		if filepath.Base(f) == ".hidden" {
			t.Errorf("hidden folder should be skipped: %q", f)
		}
	}
}
