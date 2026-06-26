package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func sampleRepos() []RepoItem {
	return []RepoItem{
		{NameWithOwner: "JinmuGo/eme", Description: "agent mission control", Private: false},
		{NameWithOwner: "JinmuGo/spotifynow", Description: "now playing", Private: true},
	}
}

func TestRepoPickerSelect(t *testing.T) {
	m := NewRepoPicker(sampleRepos())
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.Cancelled() {
		t.Fatal("unexpected cancel")
	}
	if m.Selected().NameWithOwner != "JinmuGo/eme" {
		t.Errorf("Selected = %q, want JinmuGo/eme", m.Selected().NameWithOwner)
	}
}

func TestRepoPickerCancel(t *testing.T) {
	m := NewRepoPicker(sampleRepos())
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.Cancelled() {
		t.Error("Cancelled = false, want true after Esc")
	}
}

func TestRepoPickerFilter(t *testing.T) {
	m := NewRepoPicker(sampleRepos())
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("spot")})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.Selected().NameWithOwner != "JinmuGo/spotifynow" {
		t.Errorf("Selected = %q, want JinmuGo/spotifynow", m.Selected().NameWithOwner)
	}
}
