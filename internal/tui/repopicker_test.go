package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func sampleRepos() []RepoItem {
	return []RepoItem{
		{NameWithOwner: "alderwork/eme", Description: "agent mission control", Private: false},
		{NameWithOwner: "JinmuGo/spotifynow", Description: "now playing", Private: true},
	}
}

func TestRepoPickerSelect(t *testing.T) {
	m := NewRepoPicker(sampleRepos())
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.Cancelled() {
		t.Fatal("unexpected cancel")
	}
	if m.Selected().NameWithOwner != "alderwork/eme" {
		t.Errorf("Selected = %q, want alderwork/eme", m.Selected().NameWithOwner)
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

func TestRepoPickerBox(t *testing.T) {
	m := NewRepoPicker(sampleRepos())
	if got := m.Box(); !strings.Contains(got, "alderwork/eme") {
		t.Errorf("Box() = %q, want it to list a repo", got)
	}
}

var _ overlayModal = &RepoPickerModel{}
