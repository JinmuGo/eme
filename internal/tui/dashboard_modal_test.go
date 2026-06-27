package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// wireModals installs fake picker factories so the dashboard runs its in-place modal flows
// (instead of falling back to child processes). The agent picker offers an installed
// "claude" row (the initial cursor) plus a none row; the folder picker offers two folders.
func wireModals(m *DashboardModel) {
	m.SetAgentPicker(func(sessionID, worktreeName string) *AgentPickerModel {
		return NewAgentPicker([]AgentItem{
			{Name: "claude", Command: "claude", Installed: true},
			{Name: "none", None: true, Installed: true},
		}, "")
	})
	m.SetFolderPicker(func() *FolderPickerModel {
		return NewFolderPicker([]string{"/code/alpha", "/code/beta"})
	})
	m.SetRepoFetcher(func() ([]RepoItem, error) {
		return []RepoItem{{NameWithOwner: "octo/eme"}, {NameWithOwner: "octo/x"}}, nil
	})
}

func enter() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }
func esc() tea.KeyMsg   { return tea.KeyMsg{Type: tea.KeyEsc} }
func typeText(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// sized returns a wired dashboard that has already received a window size, so its modals lay
// out before the first paint.
func sized(t *testing.T) *DashboardModel {
	t.Helper()
	m := NewDashboard(sampleViews(), nil)
	wireModals(m)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	return m
}

func TestFlowArgBuilders(t *testing.T) {
	cases := []struct {
		name string
		got  []string
		want []string
	}{
		{"worktree", worktreeCreateArgs("myapp", "feat", "claude"),
			[]string{"new", "--worktree", "myapp", "feat", "--no-switch", "--agent", "claude"}},
		{"agentSet", agentSetArgs("myapp", "feat", "codex"),
			[]string{"agent", "myapp", "feat", "--set", "codex"}},
		{"newProject", newProjectArgs("/code/x", "none"),
			[]string{"new", "/code/x", "--no-switch", "--agent", "none"}},
	}
	for _, c := range cases {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestFlowArgs_MapsPerKind(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)

	m.flow = &modalFlow{kind: flowWorktree, sessKey: "myapp", wtName: "feat"}
	if got, want := m.flowArgs("claude"), worktreeCreateArgs("myapp", "feat", "claude"); !reflect.DeepEqual(got, want) {
		t.Errorf("worktree flowArgs = %v, want %v", got, want)
	}
	m.flow = &modalFlow{kind: flowAgentOnly, sessKey: "myapp", wtName: "main"}
	if got, want := m.flowArgs("codex"), agentSetArgs("myapp", "main", "codex"); !reflect.DeepEqual(got, want) {
		t.Errorf("agentOnly flowArgs = %v, want %v", got, want)
	}
	m.flow = &modalFlow{kind: flowNewProject, folder: "/code/x"}
	if got, want := m.flowArgs("none"), newProjectArgs("/code/x", "none"); !reflect.DeepEqual(got, want) {
		t.Errorf("newProject flowArgs = %v, want %v", got, want)
	}
}

func TestCloneArgs(t *testing.T) {
	got := cloneArgs("alderwork/eme", "claude")
	want := []string{"clone", "alderwork/eme", "--no-switch", "--agent", "claude"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cloneArgs = %v, want %v", got, want)
	}
}

func TestFlowArgs_Clone(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.flow = &modalFlow{kind: flowClone, repo: "octo/eme"}
	if got, want := m.flowArgs("none"), cloneArgs("octo/eme", "none"); !reflect.DeepEqual(got, want) {
		t.Errorf("clone flowArgs = %v, want %v", got, want)
	}
}

// TestModalFlow_WorktreeCreate walks `c`: name input → agent picker → background create. The
// cursor rests on the myapp header, so the flow targets session myapp.
func TestModalFlow_WorktreeCreate(t *testing.T) {
	m := sized(t)

	m.Update(runeKey('c'))
	if _, ok := m.modal.(*InputModel); !ok {
		t.Fatalf("after c, modal = %T, want *InputModel", m.modal)
	}
	if m.flow == nil || m.flow.kind != flowWorktree || m.flow.sessKey != "myapp" {
		t.Fatalf("flow = %+v, want worktree/myapp", m.flow)
	}

	m.Update(typeText("hi"))
	m.Update(enter())
	if _, ok := m.modal.(*AgentPickerModel); !ok {
		t.Fatalf("after name, modal = %T, want *AgentPickerModel", m.modal)
	}
	if m.flow.wtName != "hi" {
		t.Fatalf("wtName = %q, want hi", m.flow.wtName)
	}
	// The argv the agent choice will fire is the create command with the typed name.
	if got, want := m.flowArgs("claude"), worktreeCreateArgs("myapp", "hi", "claude"); !reflect.DeepEqual(got, want) {
		t.Fatalf("pending args = %v, want %v", got, want)
	}

	_, cmd := m.Update(enter()) // choose claude (initial cursor)
	if m.modal != nil || m.flow != nil {
		t.Fatalf("flow should be torn down after the choice; modal=%v flow=%v", m.modal, m.flow)
	}
	if cmd == nil {
		t.Fatal("choosing the agent must dispatch the background create")
	}
}

// TestModalFlow_AgentRepick walks `A`: agent picker → background `agent --set` for the
// selected worktree.
func TestModalFlow_AgentRepick(t *testing.T) {
	m := sized(t)
	m.Update(runeKey('j')) // header → myapp/main worktree
	if m.selected() == nil {
		t.Fatal("cursor should rest on a worktree")
	}

	m.Update(runeKey('A'))
	if _, ok := m.modal.(*AgentPickerModel); !ok {
		t.Fatalf("after A, modal = %T, want *AgentPickerModel", m.modal)
	}
	if m.flow.kind != flowAgentOnly || m.flow.sessKey != "myapp" || m.flow.wtName != "main" {
		t.Fatalf("flow = %+v, want agentOnly/myapp/main", m.flow)
	}
	if got, want := m.flowArgs("claude"), agentSetArgs("myapp", "main", "claude"); !reflect.DeepEqual(got, want) {
		t.Fatalf("pending args = %v, want %v", got, want)
	}

	_, cmd := m.Update(enter())
	if m.modal != nil || cmd == nil {
		t.Fatalf("agent choice should fire and close; modal=%v cmd=%v", m.modal, cmd)
	}
}

// TestModalFlow_NewProject walks `n`: folder picker → agent picker → background create.
func TestModalFlow_NewProject(t *testing.T) {
	m := sized(t)

	m.Update(runeKey('n'))
	if _, ok := m.modal.(*FolderPickerModel); !ok {
		t.Fatalf("after n, modal = %T, want *FolderPickerModel", m.modal)
	}
	if m.flow.kind != flowNewProject {
		t.Fatalf("flow kind = %v, want flowNewProject", m.flow.kind)
	}

	m.Update(enter()) // select the first folder, /code/alpha
	if _, ok := m.modal.(*AgentPickerModel); !ok {
		t.Fatalf("after folder, modal = %T, want *AgentPickerModel", m.modal)
	}
	if m.flow.folder != "/code/alpha" {
		t.Fatalf("folder = %q, want /code/alpha", m.flow.folder)
	}
	if got, want := m.flowArgs("claude"), newProjectArgs("/code/alpha", "claude"); !reflect.DeepEqual(got, want) {
		t.Fatalf("pending args = %v, want %v", got, want)
	}

	_, cmd := m.Update(enter())
	if m.modal != nil || cmd == nil {
		t.Fatalf("agent choice should fire and close; modal=%v cmd=%v", m.modal, cmd)
	}
}

// TestModalFlow_NameCancelAborts: Esc on the worktree-name input creates nothing.
func TestModalFlow_NameCancelAborts(t *testing.T) {
	m := sized(t)
	m.Update(runeKey('c'))
	_, cmd := m.Update(esc())
	if m.modal != nil || m.flow != nil {
		t.Fatalf("esc on name must tear down the modal; modal=%v flow=%v", m.modal, m.flow)
	}
	if cmd != nil {
		t.Fatal("aborting must not dispatch a command")
	}
}

// TestModalFlow_RepickCancelAborts: Esc on the A-flow agent picker changes nothing.
func TestModalFlow_RepickCancelAborts(t *testing.T) {
	m := sized(t)
	m.Update(runeKey('j'))
	m.Update(runeKey('A'))
	_, cmd := m.Update(esc())
	if m.modal != nil {
		t.Fatal("esc on the repick picker must close the modal")
	}
	if cmd != nil {
		t.Fatal("cancelling a repick must not run agent --set")
	}
}

// TestModalFlow_CreateAgentCancelMakesBareWorktree: Esc on the create-flow agent picker still
// creates the worktree, just with no agent (--agent none) — matching the CLI, where
// cancelling the picker leaves a bare shell.
func TestModalFlow_CreateAgentCancelMakesBareWorktree(t *testing.T) {
	m := sized(t)
	m.Update(runeKey('c'))
	m.Update(typeText("hi"))
	m.Update(enter()) // → agent picker
	wantArgs := m.flowArgs("none")
	if want := worktreeCreateArgs("myapp", "hi", "none"); !reflect.DeepEqual(wantArgs, want) {
		t.Fatalf("none args = %v, want %v", wantArgs, want)
	}
	_, cmd := m.Update(esc())
	if m.modal != nil {
		t.Fatal("agent-cancel should close the modal")
	}
	if cmd == nil {
		t.Fatal("create flow must still create the worktree (bare) when the agent is cancelled")
	}
}

// TestModalFlow_ViewOverlaysDashboard: an open modal is composited over the live tree, not
// painted on a cleared screen — so the dashboard text and the dialog both appear.
func TestModalFlow_ViewOverlaysDashboard(t *testing.T) {
	m := sized(t)
	m.Update(runeKey('c'))
	out := m.View()
	if !strings.Contains(out, "Worktree name") {
		t.Error("View should show the dialog prompt")
	}
	if !strings.Contains(out, "myapp") {
		t.Error("View should still show the dashboard underneath the dialog (overlay, not clear)")
	}
	if !strings.Contains(out, "╭") {
		t.Error("View should show the dialog's rounded border")
	}
}

// TestModalFlow_FallsBackWhenUnwired: without injected factories the actions degrade to child
// processes (no in-place modal), preserving pre-modal behavior for tests/headless callers.
func TestModalFlow_FallsBackWhenUnwired(t *testing.T) {
	m := NewDashboard(sampleViews(), nil) // factories not wired
	_, cmd := m.Update(runeKey('n'))
	if m.modal != nil {
		t.Fatal("no modal should open without picker factories")
	}
	if cmd == nil {
		t.Fatal("unwired n should fall back to a child process")
	}
}

// TestModalFlow_AgentRepickFallsBackWhenUnwired: with no factories, `A` on a worktree degrades
// to the `agent … --pick` child instead of opening a modal.
func TestModalFlow_AgentRepickFallsBackWhenUnwired(t *testing.T) {
	m := NewDashboard(sampleViews(), nil) // unwired
	m.cursor = 1                          // myapp/main worktree
	if m.selected() == nil {
		t.Fatal("cursor should rest on a worktree")
	}
	_, cmd := m.Update(runeKey('A'))
	if m.modal != nil {
		t.Fatal("no modal should open without an agent-picker factory")
	}
	if cmd == nil {
		t.Fatal("unwired A should fall back to a child process")
	}
}

// TestModalFlow_WorktreeNameEmptyAborts: submitting a blank/whitespace name creates nothing —
// it tears down the modal and dispatches no command (matches the CLI, which refuses a blank
// worktree name).
func TestModalFlow_WorktreeNameEmptyAborts(t *testing.T) {
	m := sized(t)
	m.Update(runeKey('c'))
	m.Update(typeText("   ")) // whitespace only
	_, cmd := m.Update(enter())
	if m.modal != nil || m.flow != nil {
		t.Fatalf("blank name must tear down the modal; modal=%v flow=%v", m.modal, m.flow)
	}
	if cmd != nil {
		t.Fatal("blank name must not dispatch a create")
	}
}

// TestModalFlow_NewProjectAgentCancelMakesBareProject: Esc on the new-project agent picker
// still creates the project (with --agent none), the same as the worktree-create flow — both
// non-flowAgentOnly flows proceed on agent cancel.
func TestModalFlow_NewProjectAgentCancelMakesBareProject(t *testing.T) {
	m := sized(t)
	m.Update(runeKey('n'))
	m.Update(enter()) // pick /code/alpha → agent picker
	if got, want := m.flowArgs("none"), newProjectArgs("/code/alpha", "none"); !reflect.DeepEqual(got, want) {
		t.Fatalf("pending none args = %v, want %v", got, want)
	}
	_, cmd := m.Update(esc())
	if m.modal != nil {
		t.Fatal("agent-cancel should close the modal")
	}
	if cmd == nil {
		t.Fatal("new-project flow must still create the project (bare) when the agent is cancelled")
	}
}

// TestDashboard_AgentToggleOnWorktree: `a` toggles the agent in the selected worktree via a
// background child (no screen handoff). On a session header it explains the action is
// worktree-scoped instead of running anything.
func TestDashboard_AgentToggleOnWorktree(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)

	m.cursor = 1 // myapp/main worktree
	_, cmd := m.Update(runeKey('a'))
	if cmd == nil {
		t.Error("a on a worktree should dispatch the toggle child")
	}
	if m.notice != "" {
		t.Errorf("notice = %q, want empty on a worktree", m.notice)
	}

	m.cursor = 0 // myapp header
	_, cmd = m.Update(runeKey('a'))
	if cmd != nil {
		t.Error("a on a session header must not run a child")
	}
	if m.notice == "" {
		t.Error("a on a header should explain the action is for worktrees")
	}
}

// TestDashboard_AgentArgs: the toggle/pick argv targets the selected worktree by session +
// name, with --pick appended only for the catalog form.
func TestDashboard_AgentArgs(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 1 // myapp/main

	if args, ok := m.AgentArgs(false); !ok || !reflect.DeepEqual(args, []string{"agent", "myapp", "main"}) {
		t.Errorf("AgentArgs(false) = %v ok=%v, want [agent myapp main]", args, ok)
	}
	if args, ok := m.AgentArgs(true); !ok || !reflect.DeepEqual(args, []string{"agent", "myapp", "main", "--pick"}) {
		t.Errorf("AgentArgs(true) = %v ok=%v, want [agent myapp main --pick]", args, ok)
	}

	m.cursor = 0 // header — no worktree selected
	if _, ok := m.AgentArgs(false); ok {
		t.Error("AgentArgs on a header should report ok=false")
	}
}

// TestModalFlow_Clone walks `g`: loading modal → repo picker (after load) → agent picker →
// background clone.
func TestModalFlow_Clone(t *testing.T) {
	m := sized(t)

	_, cmd := m.Update(runeKey('g'))
	if _, ok := m.modal.(*LoadingModal); !ok {
		t.Fatalf("after g, modal = %T, want *LoadingModal", m.modal)
	}
	if m.flow == nil || m.flow.kind != flowClone {
		t.Fatalf("flow = %+v, want flowClone", m.flow)
	}
	if cmd == nil {
		t.Fatal("g must dispatch the repo-load cmd")
	}

	m.Update(reposLoadedMsg{repos: []RepoItem{{NameWithOwner: "octo/eme"}, {NameWithOwner: "octo/x"}}})
	if _, ok := m.modal.(*RepoPickerModel); !ok {
		t.Fatalf("after load, modal = %T, want *RepoPickerModel", m.modal)
	}

	m.Update(enter()) // pick the first repo, octo/eme
	if _, ok := m.modal.(*AgentPickerModel); !ok {
		t.Fatalf("after repo, modal = %T, want *AgentPickerModel", m.modal)
	}
	if m.flow.repo != "octo/eme" {
		t.Fatalf("repo = %q, want octo/eme", m.flow.repo)
	}
	if got, want := m.flowArgs("claude"), cloneArgs("octo/eme", "claude"); !reflect.DeepEqual(got, want) {
		t.Fatalf("pending args = %v, want %v", got, want)
	}

	_, cmd = m.Update(enter()) // choose claude
	if m.modal != nil || m.flow != nil {
		t.Fatalf("flow should tear down after the choice; modal=%v flow=%v", m.modal, m.flow)
	}
	if cmd == nil {
		t.Fatal("choosing the agent must dispatch the background clone")
	}
}

// TestModalFlow_CloneLoadError: a failed repo load closes the modal and shows a notice.
func TestModalFlow_CloneLoadError(t *testing.T) {
	m := sized(t)
	m.Update(runeKey('g'))
	m.Update(reposLoadedMsg{err: errors.New("gh: not authenticated")})
	if m.modal != nil || m.flow != nil {
		t.Fatalf("load error should tear down the modal; modal=%v flow=%v", m.modal, m.flow)
	}
	if m.notice == "" {
		t.Error("load error should set a notice")
	}
}

// TestModalFlow_CloneFallsBackWhenUnwired: without a fetcher/agent picker, `g` degrades to the
// child process.
func TestModalFlow_CloneFallsBackWhenUnwired(t *testing.T) {
	m := NewDashboard(sampleViews(), nil) // unwired
	_, cmd := m.Update(runeKey('g'))
	if m.modal != nil {
		t.Fatal("no modal should open without a fetcher/agent picker")
	}
	if cmd == nil {
		t.Fatal("unwired g should fall back to a child process")
	}
}

// TestModalFlow_CloneRepoCancelAborts: Esc on the repo picker creates nothing.
func TestModalFlow_CloneRepoCancelAborts(t *testing.T) {
	m := sized(t)
	m.Update(runeKey('g'))
	m.Update(reposLoadedMsg{repos: []RepoItem{{NameWithOwner: "octo/eme"}}})
	_, cmd := m.Update(esc())
	if m.modal != nil || m.flow != nil {
		t.Fatalf("esc on repo picker must tear down; modal=%v flow=%v", m.modal, m.flow)
	}
	if cmd != nil {
		t.Fatal("cancelling the repo pick must not dispatch a clone")
	}
}
