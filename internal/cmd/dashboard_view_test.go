package cmd

import (
	"testing"
	"time"

	"github.com/alderwork/eme/internal/git"
	"github.com/alderwork/eme/internal/runner"
	"github.com/alderwork/eme/internal/state"
	"github.com/alderwork/eme/internal/tmux"
	"github.com/alderwork/eme/internal/tui"
)

func TestClassifyStatus(t *testing.T) {
	cases := []struct {
		name    string
		info    tmux.PaneInfo
		present bool
		last    string
		want    tui.AgentStatus
	}{
		{"never ran", tmux.PaneInfo{}, false, "", tui.StatusIdle},
		{"ran, window gone", tmux.PaneInfo{}, false, "claude", tui.StatusExited},
		// Live pane: status reads the FOREGROUND process. The agent runs under a
		// different name (claude => node), so a non-shell foreground means working;
		// a shell prompt means idle — even when an agent ran earlier (it has exited).
		{"running, node-named", tmux.PaneInfo{Dead: false, Command: "node"}, true, "claude", tui.StatusWorking},
		{"running, no record", tmux.PaneInfo{Dead: false, Command: "node"}, true, "", tui.StatusWorking},
		{"agent exited, back at prompt", tmux.PaneInfo{Dead: false, Command: "zsh"}, true, "claude", tui.StatusIdle},
		{"login shell prompt", tmux.PaneInfo{Dead: false, Command: "-zsh"}, true, "claude", tui.StatusIdle},
		{"alive shell, never ran", tmux.PaneInfo{Dead: false, Command: "bash"}, true, "", tui.StatusIdle},
		// An empty/unresolved foreground biases to idle, not a phantom running agent.
		{"empty foreground", tmux.PaneInfo{Dead: false, Command: ""}, true, "claude", tui.StatusIdle},
		// pane_dead is rare now (only a manually-killed/exited pane) but still maps.
		{"clean exit (dead pane)", tmux.PaneInfo{Dead: true, DeadStatus: 0}, true, "claude", tui.StatusExited},
		{"crash (dead pane)", tmux.PaneInfo{Dead: true, DeadStatus: 3}, true, "claude", tui.StatusCrashed},
		// A hook-pushed @eme_state refines the live non-shell case into a precise state.
		{"hook: waiting", tmux.PaneInfo{Dead: false, Command: "node", EmeState: "waiting"}, true, "claude", tui.StatusWaiting},
		{"hook: working", tmux.PaneInfo{Dead: false, Command: "node", EmeState: "working"}, true, "claude", tui.StatusWorking},
		{"hook: done while agent alive", tmux.PaneInfo{Dead: false, Command: "node", EmeState: "done"}, true, "claude", tui.StatusIdle},
		{"hook: crashed while agent alive", tmux.PaneInfo{Dead: false, Command: "node", EmeState: "crashed"}, true, "claude", tui.StatusCrashed},
		{"unknown hook value falls back to working", tmux.PaneInfo{Dead: false, Command: "node", EmeState: "banana"}, true, "claude", tui.StatusWorking},
		// Ground-truth precedence: a shell prompt is idle even if a stale @eme_state lingers
		// (the agent crashed/quit and returned to the shell — the option was never cleared).
		{"shell prompt beats stale hook state", tmux.PaneInfo{Dead: false, Command: "zsh", EmeState: "working"}, true, "claude", tui.StatusIdle},
	}
	for _, c := range cases {
		if got := classifyStatus(c.info, c.present, c.last); got != c.want {
			t.Errorf("%s: classifyStatus = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestIsShellCommand_HonorsUserShellAndModernShells: a pane is idle when its
// foreground is any common shell, the user's own $SHELL (even if exotic), or empty
// (unresolved). Anything else (an agent / a running command) is not a shell.
func TestIsShellCommand_HonorsUserShellAndModernShells(t *testing.T) {
	t.Setenv("SHELL", "/usr/bin/exoticsh") // not in the static set
	for _, c := range []struct {
		cmd  string
		want bool
	}{
		{"zsh", true}, {"-zsh", true}, {"/bin/bash", true}, // common + login + path
		{"fish", true}, {"nu", true}, {"pwsh", true}, // modern shells
		{"exoticsh", true},             // the user's own $SHELL basename
		{"", true},                     // empty/unresolved foreground biases to idle
		{"node", false},                // claude runs as node — a working agent
		{"vim", false}, {"git", false}, // running commands are not idle
	} {
		if got := isShellCommand(c.cmd); got != c.want {
			t.Errorf("isShellCommand(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}

func TestShortLocation(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"/", ""},
		{"/a", "a"},
		{"/a/b", "a/b"},
		{"/Users/jinmu/Programming/new/eme", "…/new/eme"},
		{"/Users/jinmu/Programming/new/eme.worktrees/gege", "…/eme.worktrees/gege"},
		{"relative/path/here", "…/path/here"},
		{"/x/y/z/", "…/y/z"},
	}
	for _, c := range cases {
		if got := shortLocation(c.in); got != c.want {
			t.Errorf("shortLocation(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAgentLabel(t *testing.T) {
	cases := map[string]string{
		"claude --dangerously": "claude",
		"/usr/bin/opencode":    "opencode",
		"":                     "",
	}
	for cmd, want := range cases {
		if got := agentLabel(&state.Worktree{LastAgentCommand: cmd}); got != want {
			t.Errorf("agentLabel(%q) = %q, want %q", cmd, got, want)
		}
	}
}

// TestBuildStatusViews_SkipsGitDiff locks T3: the status-only path classifies agent
// status but never shells out to git (no DiffStat), so the ticker stays cheap.
func TestBuildStatusViews_SkipsGitDiff(t *testing.T) {
	mock := runner.NewMock()
	git.Runner = mock
	defer func() { git.Runner = runner.Default }()

	sessions := []state.Session{{
		ID: "myapp", DisplayName: "myapp", Root: "/code/myapp",
		Worktrees: []state.Worktree{
			{Name: "feat", Branch: "feat/x", TmuxWindowID: "@2", LastAgentCommand: "claude"},
		},
	}}
	snap := map[string]tmux.PaneInfo{"@2": {Dead: false, Command: "node"}}

	views := buildStatusViews(sessions, snap, time.Now(), 2*time.Minute)
	if len(mock.Calls) != 0 {
		t.Errorf("status-only build must not shell out to git, made %d call(s): %+v", len(mock.Calls), mock.Calls)
	}
	w := views[0].Worktrees[0]
	if w.Status != tui.StatusWorking {
		t.Errorf("status = %v, want StatusWorking", w.Status)
	}
	if w.HasDiff {
		t.Error("status-only build must not populate diff")
	}
}

func TestBuildSessionViews_MapsFields(t *testing.T) {
	git.Runner = runner.NewMock()
	defer func() { git.Runner = runner.Default }()

	sessions := []state.Session{{
		ID:          "myapp-abc",
		DisplayName: "myapp",
		Root:        "/code/myapp",
		Worktrees: []state.Worktree{
			{Name: "main", Branch: "main", TmuxWindowID: "@1", Path: "/code/myapp/main"},
			{Name: "feat", Branch: "feat/x", TmuxWindowID: "@2", LastAgentCommand: "claude"},
		},
	}}

	// @2 (feat) has a live pane running the agent (reported as node) → running.
	// @1 (main) recorded no agent → idle, regardless of its live shell pane.
	snap := map[string]tmux.PaneInfo{
		"@1": {Dead: false, Command: "zsh"},
		"@2": {Dead: false, Command: "node"},
	}

	views := buildSessionViews(sessions, snap, time.Now(), 2*time.Minute)
	if len(views) != 1 || len(views[0].Worktrees) != 2 {
		t.Fatalf("unexpected shape: %+v", views)
	}
	main := views[0].Worktrees[0]
	if !main.IsMain || main.Status != tui.StatusIdle || main.SessionID != "myapp-abc" {
		t.Errorf("main view wrong: %+v", main)
	}
	if main.Location != "…/myapp/main" {
		t.Errorf("main.Location = %q, want \"…/myapp/main\"", main.Location)
	}
	feat := views[0].Worktrees[1]
	if feat.IsMain || feat.Status != tui.StatusWorking || feat.AgentLabel != "claude" {
		t.Errorf("feat view wrong: %+v", feat)
	}
}

// TestBuildSessionViews_PlainLayoutSetsIsPlain locks the wiring the dashboard's
// create-worktree gate reads: a LayoutPlain session surfaces IsPlain=true, while a
// git-backed one (in-place here) stays false.
func TestBuildSessionViews_PlainLayoutSetsIsPlain(t *testing.T) {
	git.Runner = runner.NewMock()
	defer func() { git.Runner = runner.Default }()

	sessions := []state.Session{
		{
			ID: "repo", DisplayName: "repo", Root: "/code/repo", Layout: state.LayoutInPlace,
			Worktrees: []state.Worktree{{Name: "main", TmuxWindowID: "@1"}},
		},
		{
			ID: "docs", DisplayName: "docs", Root: "/notes/docs", Layout: state.LayoutPlain,
			Worktrees: []state.Worktree{{Name: "main", TmuxWindowID: "@2"}},
		},
	}
	snap := map[string]tmux.PaneInfo{"@1": {Command: "zsh"}, "@2": {Command: "zsh"}}

	views := buildSessionViews(sessions, snap, time.Now(), 2*time.Minute)
	if views[0].IsPlain {
		t.Errorf("in-place repo should not be plain: %+v", views[0])
	}
	if !views[1].IsPlain {
		t.Errorf("plain folder should set IsPlain: %+v", views[1])
	}
}

func TestBuildViews_DerivesAgeAndQuiet(t *testing.T) {
	now := time.Unix(1_750_000_600, 0) // 600s after the stamps below
	sessions := []state.Session{{
		ID: "s1", DisplayName: "proj", Layout: state.LayoutNestedBare,
		Worktrees: []state.Worktree{
			{Name: "fresh", Path: "/p/fresh", TmuxWindowID: "@1"},
			{Name: "quiet", Path: "/p/quiet", TmuxWindowID: "@2"},
			{Name: "bare", Path: "/p/bare", TmuxWindowID: "@3"},
		},
	}}
	snap := map[string]tmux.PaneInfo{
		"@1": {Command: "node", EmeState: "working", EmeStateAt: 1_750_000_580}, // 20s ago → not quiet
		"@2": {Command: "node", EmeState: "working", EmeStateAt: 1_750_000_300}, // 300s ago → quiet
		"@3": {Command: "node"},                                                 // no hook → no age/quiet
	}
	views := buildViews(sessions, snap, false, now, 2*time.Minute)
	wts := views[0].Worktrees
	if wts[0].AgeLabel != "20s" || wts[0].Quiet {
		t.Errorf("fresh: age=%q quiet=%v, want 20s / not quiet", wts[0].AgeLabel, wts[0].Quiet)
	}
	if wts[1].AgeLabel != "5m" || !wts[1].Quiet {
		t.Errorf("quiet: age=%q quiet=%v, want 5m / quiet", wts[1].AgeLabel, wts[1].Quiet)
	}
	if wts[2].Hooked || wts[2].AgeLabel != "" || !wts[2].StateChangedAt.IsZero() {
		t.Errorf("bare: hooked=%v age=%q — want unhooked, no age", wts[2].Hooked, wts[2].AgeLabel)
	}
}

// TestBuildViews_UnhookedQuietFromActivity locks ET1: an un-hooked working agent (no
// @eme_state) derives its age and the soft "quiet" hint from window_activity — recent output
// reads as plain working, a long silence dims to quiet. Crucially the silent agent stays
// StatusWorking, never StatusWaiting: an un-hooked guess dims, it never lights the beacon.
func TestBuildViews_UnhookedQuietFromActivity(t *testing.T) {
	now := time.Unix(1_750_000_600, 0) // 600s after the activity stamps below
	sessions := []state.Session{{
		ID: "s1", DisplayName: "proj", Layout: state.LayoutNestedBare,
		Worktrees: []state.Worktree{
			{Name: "busy", Path: "/p/busy", TmuxWindowID: "@1"},
			{Name: "stalled", Path: "/p/stalled", TmuxWindowID: "@2"},
		},
	}}
	snap := map[string]tmux.PaneInfo{
		"@1": {Command: "node", Activity: 1_750_000_580}, // output 20s ago → working, not quiet
		"@2": {Command: "node", Activity: 1_750_000_300}, // silent 300s   → quiet hint
	}
	views := buildViews(sessions, snap, false, now, 2*time.Minute)
	wts := views[0].Worktrees
	if wts[0].Hooked || wts[1].Hooked {
		t.Fatalf("both agents are un-hooked; Hooked must be false: %+v", wts)
	}
	if wts[0].AgeLabel != "20s" || wts[0].Quiet {
		t.Errorf("busy: age=%q quiet=%v, want 20s / not quiet (recently active)", wts[0].AgeLabel, wts[0].Quiet)
	}
	if wts[1].AgeLabel != "5m" || !wts[1].Quiet {
		t.Errorf("stalled: age=%q quiet=%v, want 5m / quiet (silent ≥ 2m)", wts[1].AgeLabel, wts[1].Quiet)
	}
	if wts[1].Status != tui.StatusWorking {
		t.Errorf("stalled: status=%v, want StatusWorking — silence is a dim hint, never the amber beacon", wts[1].Status)
	}
}

func TestBuildSessionViews_CarriesCaffeinateMode(t *testing.T) {
	sessions := []state.Session{{
		ID: "p-1", DisplayName: "p", Root: "/p", TmuxName: "p",
		CaffeinateMode: "auto",
		Worktrees:      []state.Worktree{{Name: "main", TmuxWindowID: "@1"}},
	}}
	views := buildSessionViews(sessions, map[string]tmux.PaneInfo{}, time.Now(), 0)
	if len(views) != 1 || views[0].Caffeinate != "auto" {
		t.Fatalf("Caffeinate = %q, want auto", views[0].Caffeinate)
	}
}

func TestFormatAge(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{-time.Second, ""}, {0, "0s"}, {45 * time.Second, "45s"}, {90 * time.Second, "1m"},
		{59 * time.Minute, "59m"}, {60 * time.Minute, "1h"}, {23 * time.Hour, "23h"},
		{24 * time.Hour, "1d"}, {400 * time.Hour, "16d"},
	}
	for _, c := range cases {
		if got := formatAge(c.d); got != c.want {
			t.Errorf("formatAge(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
