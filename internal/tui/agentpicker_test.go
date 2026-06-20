package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func sampleItems() []AgentItem {
	return []AgentItem{
		{Name: "claude", Command: "claude", Installed: true},
		{Name: "codex", Command: "codex", Installed: false},
		{Name: "opencode", Command: "opencode", Installed: true},
		{Name: "none", None: true, Installed: true},
	}
}

func key(m *AgentPickerModel, t tea.KeyType) *AgentPickerModel {
	model, _ := m.Update(tea.KeyMsg{Type: t})
	return model.(*AgentPickerModel)
}

func TestAgentPicker_EnterSelectsInstalled(t *testing.T) {
	m := NewAgentPicker(sampleItems(), "claude") // cursor starts on claude
	m = key(m, tea.KeyEnter)
	sel, ok := m.Chosen()
	if !ok || sel.Name != "claude" {
		t.Fatalf("Chosen() = %+v, %v; want claude, true", sel, ok)
	}
	if m.Cancelled() {
		t.Errorf("Cancelled() = true, want false")
	}
}

func TestAgentPicker_SkipsUninstalledOnNavigation(t *testing.T) {
	m := NewAgentPicker(sampleItems(), "claude") // index 0 (claude)
	m = key(m, tea.KeyDown)                      // must skip codex (index 1) → opencode (index 2)
	m = key(m, tea.KeyEnter)
	sel, ok := m.Chosen()
	if !ok || sel.Name != "opencode" {
		t.Fatalf("after Down+Enter Chosen() = %+v, %v; want opencode, true", sel, ok)
	}
}

func TestAgentPicker_SelectsNone(t *testing.T) {
	m := NewAgentPicker(sampleItems(), "claude")
	m = key(m, tea.KeyDown) // opencode
	m = key(m, tea.KeyDown) // none
	m = key(m, tea.KeyEnter)
	sel, ok := m.Chosen()
	if !ok || !sel.None {
		t.Fatalf("Chosen() = %+v, %v; want none row, true", sel, ok)
	}
}

func TestAgentPicker_EscCancels(t *testing.T) {
	m := NewAgentPicker(sampleItems(), "claude")
	m = key(m, tea.KeyEsc)
	if _, ok := m.Chosen(); ok {
		t.Errorf("Chosen() ok = true after Esc, want false")
	}
	if !m.Cancelled() {
		t.Errorf("Cancelled() = false, want true")
	}
}

func TestNewAgentPicker_DefaultHighlightSkipsUninstalled(t *testing.T) {
	// default points at an uninstalled agent → cursor falls to first installed.
	m := NewAgentPicker(sampleItems(), "codex")
	if got := m.items[m.cursor].Name; got != "claude" {
		t.Errorf("initial cursor on %q, want first installed 'claude'", got)
	}
}
