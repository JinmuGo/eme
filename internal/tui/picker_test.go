package tui

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func typeInto(m *FolderPickerModel, s string) *FolderPickerModel {
	for _, c := range s {
		var model tea.Model = m
		model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{c}})
		m = model.(*FolderPickerModel)
	}
	return m
}

// TestFolderPicker_DoesNotMutateItems guards against the slice-aliasing bug
// where updateFilter wrote into the backing array shared with m.items,
// corrupting the source list and producing duplicate entries.
func TestFolderPicker_DoesNotMutateItems(t *testing.T) {
	items := []string{
		"/Users/jinmu/Programming",
		"/Users/jinmu/Pictures",
		"/Users/jinmu/Public",
		"/Users/jinmu/Movies",
		"/Users/jinmu/Music",
		"/Users/jinmu/Desktop",
		"/Users/jinmu/Documents",
		"/Users/jinmu/Downloads",
		"/Users/jinmu/Library",
		"/Users/jinmu/go",
		"/Users/jinmu/Applications",
		"/Users/jinmu/Parallels",
	}
	original := append([]string(nil), items...)

	m := typeInto(NewFolderPicker(items), "programming")

	if !reflect.DeepEqual(items, original) {
		t.Fatalf("source items mutated by filtering:\n got  %v\n want %v", items, original)
	}
	if len(m.filtered) != 1 || m.filtered[0] != "/Users/jinmu/Programming" {
		t.Fatalf("filter result wrong: %v", m.filtered)
	}
}

// TestFolderPicker_NoDuplicatesAfterEditing types, clears, and retypes to
// exercise the q=="" re-alias path, then checks for duplicate results.
func TestFolderPicker_NoDuplicatesAfterEditing(t *testing.T) {
	items := []string{
		"/Users/jinmu/Programming",
		"/Users/jinmu/Projects",
		"/Users/jinmu/Pictures",
		"/Users/jinmu/Public",
		"/Users/jinmu/Documents",
	}
	m := NewFolderPicker(items)
	m = typeInto(m, "pro")
	// clear the input (backspace x3) then retype
	for range 3 {
		var model tea.Model = m
		model, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		m = model.(*FolderPickerModel)
	}
	m = typeInto(m, "p")

	seen := map[string]bool{}
	for _, f := range m.filtered {
		if seen[f] {
			t.Fatalf("duplicate entry in filtered: %q\nfull: %v", f, m.filtered)
		}
		seen[f] = true
	}
}
