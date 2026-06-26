package git

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/JinmuGo/eme/internal/runner"
)

func TestSetFetchRefspec(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("git", []string{"-C", "/r/.bare", "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"}, "", "", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()
	if err := SetFetchRefspec("/r/.bare"); err != nil {
		t.Fatalf("SetFetchRefspec: %v", err)
	}
}

func TestFetch(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("git", []string{"-C", "/r/.bare", "fetch", "origin"}, "", "", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()
	if err := Fetch("/r/.bare"); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
}

func TestDefaultBranch(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("git", []string{"-C", "/r/.bare", "symbolic-ref", "--short", "HEAD"}, "master\n", "", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()
	b, err := DefaultBranch("/r/.bare")
	if err != nil {
		t.Fatalf("DefaultBranch: %v", err)
	}
	if b != "master" {
		t.Errorf("DefaultBranch = %q, want master", b)
	}
}

func TestSetUpstream(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("git", []string{"-C", "/r/main", "branch", "--set-upstream-to=origin/main", "main"}, "", "", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()
	if err := SetUpstream("/r/main", "main", "origin/main"); err != nil {
		t.Fatalf("SetUpstream: %v", err)
	}
}

func TestListLocalBranches(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("git", []string{"-C", "/r/.bare", "for-each-ref", "--format=%(refname:short)", "refs/heads/"}, "main\ndevelop\n", "", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()
	got, err := ListLocalBranches("/r/.bare")
	if err != nil {
		t.Fatalf("ListLocalBranches: %v", err)
	}
	if len(got) != 2 || got[0] != "main" || got[1] != "develop" {
		t.Errorf("ListLocalBranches = %v, want [main develop]", got)
	}
}

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

// TestWorktreeAddAt_SurfacesGitStderr guards the diagnosability fix: when git fails,
// WorktreeAddAt must carry git's own stderr (e.g. "cannot lock ref ...") rather than a
// bare "exit status 255", so the dashboard error tells the user what actually happened.
func TestWorktreeAddAt_SurfacesGitStderr(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("git", []string{"-C", "/repo", "worktree", "add", "-b", "feat", "/repo.worktrees/feat"},
		"", "fatal: cannot lock ref 'refs/heads/feat': 'refs/heads/feat/x' exists; cannot create 'refs/heads/feat'",
		fmt.Errorf("exit status 255"))
	Runner = mock
	defer func() { Runner = runner.Default }()

	err := WorktreeAddAt("/repo", "/repo.worktrees/feat", "", true)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "cannot lock ref") {
		t.Errorf("error must surface git's stderr, got %q", err.Error())
	}
}

// TestWorktreeAddAt_FallsBackToErrWhenNoStderr verifies the exit status is still shown
// when git emits no stderr (so the error is never empty/uninformative).
func TestWorktreeAddAt_FallsBackToErrWhenNoStderr(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("git", []string{"-C", "/repo", "worktree", "add", "-b", "feat", "/repo.worktrees/feat"},
		"", "", fmt.Errorf("exit status 128"))
	Runner = mock
	defer func() { Runner = runner.Default }()

	err := WorktreeAddAt("/repo", "/repo.worktrees/feat", "", true)
	if err == nil || !strings.Contains(err.Error(), "exit status 128") {
		t.Errorf("expected fallback to raw exit status, got %v", err)
	}
}

// TestUnpushedCommitCount parses the rev-list count of local-only commits — the
// "history that would be lost on delete" signal for a nested-bare project.
func TestUnpushedCommitCount(t *testing.T) {
	args := []string{"-C", "/proj/.bare", "rev-list", "--all", "--not", "--remotes", "--count"}

	t.Run("counts local-only commits", func(t *testing.T) {
		mock := runner.NewMock()
		mock.Set("git", args, "3\n", "", nil)
		Runner = mock
		defer func() { Runner = runner.Default }()

		n, err := UnpushedCommitCount("/proj/.bare")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n != 3 {
			t.Errorf("count = %d, want 3", n)
		}
	})

	t.Run("fully pushed reports zero", func(t *testing.T) {
		mock := runner.NewMock()
		mock.Set("git", args, "0\n", "", nil)
		Runner = mock
		defer func() { Runner = runner.Default }()

		n, err := UnpushedCommitCount("/proj/.bare")
		if err != nil || n != 0 {
			t.Errorf("n=%d err=%v, want 0, nil", n, err)
		}
	})

	t.Run("git error propagates", func(t *testing.T) {
		mock := runner.NewMock()
		mock.Set("git", args, "", "fatal: bad repo", fmt.Errorf("exit status 128"))
		Runner = mock
		defer func() { Runner = runner.Default }()

		if _, err := UnpushedCommitCount("/proj/.bare"); err == nil {
			t.Error("expected error to propagate")
		}
	})
}

func TestBranchDFConflict(t *testing.T) {
	cases := []struct {
		desc       string
		ignorecase string
		refs       string
		name       string
		wantRef    string
		wantOK     bool
	}{
		{"forward: name blocked by a sub-branch", "false",
			"feat/design-polish\nfeat/gallery\nmain", "feat", "feat/design-polish", true},
		{"reverse: name blocked by an ancestor branch", "false",
			"feat\nmain", "feat/x", "feat", true},
		{"no conflict for a free name", "false",
			"feat/x\nmain\nbugfix-1", "bugfix", "", false},
		{"exact existing branch is NOT a D/F conflict (handled elsewhere)", "false",
			"main\nfeat", "feat", "", false},
		{"case-insensitive FS folds the forward match (macOS bug)", "true",
			"feat/x\nmain", "Feat", "feat/x", true},
		{"case-sensitive FS does NOT fold (distinct names git allows)", "false",
			"feat/x\nmain", "Feat", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			mock := runner.NewMock()
			mock.Set("git", []string{"-C", "/repo", "for-each-ref", "--format=%(refname:short)", "refs/heads/"},
				tc.refs, "", nil)
			mock.Set("git", []string{"-C", "/repo", "config", "--get", "core.ignorecase"},
				tc.ignorecase, "", nil)
			Runner = mock
			defer func() { Runner = runner.Default }()
			got, ok := BranchDFConflict("/repo", tc.name)
			if ok != tc.wantOK || got != tc.wantRef {
				t.Errorf("BranchDFConflict(%q) = (%q, %v), want (%q, %v)", tc.name, got, ok, tc.wantRef, tc.wantOK)
			}
		})
	}
}

func TestRemoteBranchExists(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("git", []string{"-C", "/repo", "for-each-ref", "--format=%(refname)", "refs/remotes/"},
		"refs/remotes/origin/main\nrefs/remotes/origin/feat/foo\nrefs/remotes/upstream/dev", "", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()
	cases := []struct {
		name string
		want bool
	}{
		{"feat/foo", true}, // origin/feat/foo (multi-segment)
		{"dev", true},      // upstream/dev (non-origin remote)
		{"main", true},
		{"missing", false},
		{"origin/main", false}, // the remote segment is not part of the branch name
	}
	for _, tc := range cases {
		if got := RemoteBranchExists("/repo", tc.name); got != tc.want {
			t.Errorf("RemoteBranchExists(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRemoteCarriersOf(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("git", []string{"-C", "/repo", "for-each-ref", "--format=%(refname)", "refs/remotes/"},
		"refs/remotes/origin/shared\nrefs/remotes/upstream/shared\nrefs/remotes/origin/solo", "", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()
	// "shared" lives in two distinct remotes — DWIM is ambiguous.
	if got := RemoteCarriersOf("/repo", "shared"); len(got) != 2 || got[0] != "origin" || got[1] != "upstream" {
		t.Errorf("RemoteCarriersOf(shared) = %v, want [origin upstream]", got)
	}
	if got := RemoteCarriersOf("/repo", "solo"); len(got) != 1 || got[0] != "origin" {
		t.Errorf("RemoteCarriersOf(solo) = %v, want [origin]", got)
	}
	if got := RemoteCarriersOf("/repo", "absent"); len(got) != 0 {
		t.Errorf("RemoteCarriersOf(absent) = %v, want none", got)
	}
}

func TestBranchCheckedOutAt(t *testing.T) {
	mock := runner.NewMock()
	out := "worktree /repo\nHEAD a1\nbranch refs/heads/main\n\n" +
		"worktree /repo.worktrees/feat\nHEAD b2\nbranch refs/heads/feat/foo\n\n"
	mock.Set("git", []string{"-C", "/repo", "worktree", "list", "--porcelain"}, out, "", nil)
	mock.Set("git", []string{"-C", "/repo", "config", "--get", "core.ignorecase"}, "false", "", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()
	if p, ok := BranchCheckedOutAt("/repo", "feat/foo"); !ok || p != "/repo.worktrees/feat" {
		t.Errorf("BranchCheckedOutAt(feat/foo) = (%q, %v), want (/repo.worktrees/feat, true)", p, ok)
	}
	if p, ok := BranchCheckedOutAt("/repo", "main"); !ok || p != "/repo" {
		t.Errorf("BranchCheckedOutAt(main) = (%q, %v), want (/repo, true)", p, ok)
	}
	if _, ok := BranchCheckedOutAt("/repo", "not-out"); ok {
		t.Error("a branch in no worktree must report not-checked-out")
	}
	// Case-sensitive filesystem: a case-mismatched name is a genuinely different branch.
	if _, ok := BranchCheckedOutAt("/repo", "Main"); ok {
		t.Error("with core.ignorecase=false, 'Main' must not match 'main'")
	}
}

// TestBranchCheckedOutAt_FoldsCaseWhenIgnoreCase guards the detection gap behind the
// "existing branch fails to create" bug: on a case-insensitive filesystem git resolves
// refs case-insensitively, so BranchExists (an FS-backed show-ref) sees "Feat-A" as the
// stored "feat-a", but BranchCheckedOutAt compared names byte-for-byte and missed it —
// letting createWorktree fall through to `git worktree add`, which then fails with a raw
// "already used by worktree". When core.ignorecase is set, the comparison must fold too.
func TestBranchCheckedOutAt_FoldsCaseWhenIgnoreCase(t *testing.T) {
	mock := runner.NewMock()
	out := "worktree /repo\nHEAD a1\nbranch refs/heads/main\n\n" +
		"worktree /repo.worktrees/feat-a\nHEAD b2\nbranch refs/heads/feat-a\n\n"
	mock.Set("git", []string{"-C", "/repo", "worktree", "list", "--porcelain"}, out, "", nil)
	mock.Set("git", []string{"-C", "/repo", "config", "--get", "core.ignorecase"}, "true", "", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()

	if p, ok := BranchCheckedOutAt("/repo", "Feat-A"); !ok || p != "/repo.worktrees/feat-a" {
		t.Errorf("BranchCheckedOutAt(Feat-A) with ignorecase = (%q, %v), want (/repo.worktrees/feat-a, true)", p, ok)
	}
	// An exact match still wins without consulting the (folded) fallback.
	if p, ok := BranchCheckedOutAt("/repo", "feat-a"); !ok || p != "/repo.worktrees/feat-a" {
		t.Errorf("BranchCheckedOutAt(feat-a) = (%q, %v), want (/repo.worktrees/feat-a, true)", p, ok)
	}
}

// TestBranchCheckedOutAt_IgnoresPrunable guards the "existing branch fails to create"
// bug: a worktree whose directory was deleted out from under git leaves a *prunable*
// admin entry that still names its branch. That entry is NOT a live checkout, so it must
// not be reported as one — otherwise createWorktree refuses to reuse the branch ("already
// checked out at <gone path>") instead of pruning the dead entry and checking it back out.
func TestBranchCheckedOutAt_IgnoresPrunable(t *testing.T) {
	mock := runner.NewMock()
	out := "worktree /repo\nHEAD a1\nbranch refs/heads/main\n\n" +
		"worktree /repo.worktrees/hi\nHEAD b2\nbranch refs/heads/hi\nprunable gitdir file points to non-existent location\n\n"
	mock.Set("git", []string{"-C", "/repo", "worktree", "list", "--porcelain"}, out, "", nil)
	mock.Set("git", []string{"-C", "/repo", "config", "--get", "core.ignorecase"}, "false", "", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()

	if p, ok := BranchCheckedOutAt("/repo", "hi"); ok {
		t.Errorf("BranchCheckedOutAt(hi) on a prunable worktree = (%q, %v), want ('', false)", p, ok)
	}
	// The live worktree is still reported.
	if _, ok := BranchCheckedOutAt("/repo", "main"); !ok {
		t.Error("a live worktree must still report checked-out")
	}
}

func TestBranchesUnderNamespace(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("git", []string{"-C", "/repo", "for-each-ref", "--format=%(refname:short)", "refs/heads/"},
		"feat/a\nfeat/b\nmain\nfeature", "", nil)
	mock.Set("git", []string{"-C", "/repo", "config", "--get", "core.ignorecase"}, "false", "", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()
	got := BranchesUnderNamespace("/repo", "feat")
	// "feature" is NOT under the feat/ namespace; only feat/a and feat/b are.
	if len(got) != 2 || got[0] != "feat/a" || got[1] != "feat/b" {
		t.Errorf("BranchesUnderNamespace(feat) = %v, want [feat/a feat/b]", got)
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
