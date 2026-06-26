package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestLoadingModalCancel(t *testing.T) {
	m := NewLoadingModal("Loading…")
	if m.Cancelled() {
		t.Fatal("new modal should not be cancelled")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.Cancelled() {
		t.Error("Esc should cancel the loading modal")
	}
}

func TestLoadingModalRendersMessage(t *testing.T) {
	m := NewLoadingModal("Loading your GitHub repos…")
	if got := m.Box(); !strings.Contains(got, "Loading your GitHub repos") {
		t.Errorf("Box() = %q, want it to contain the message", got)
	}
}
