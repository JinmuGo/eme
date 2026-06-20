package config

import "testing"

func TestWorktreeDirFor_DefaultSibling(t *testing.T) {
	got, err := WorktreeDirFor("{repo}.worktrees", "/p/myapp")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "/p/myapp.worktrees" {
		t.Errorf("got %q", got)
	}
}

func TestWorktreeDirFor_RejectsAbsolute(t *testing.T) {
	if _, err := WorktreeDirFor("/abs/{repo}", "/p/myapp"); err == nil {
		t.Errorf("expected rejection of absolute template")
	}
}

func TestWorktreeDirFor_RejectsParentEscape(t *testing.T) {
	if _, err := WorktreeDirFor("../{repo}.wt", "/p/myapp"); err == nil {
		t.Errorf("expected rejection of parent-escaping template")
	}
}

func TestDefault_WorktreeTemplate(t *testing.T) {
	if Default().Worktree.DirTemplate != "{repo}.worktrees" {
		t.Errorf("default template = %q", Default().Worktree.DirTemplate)
	}
}
