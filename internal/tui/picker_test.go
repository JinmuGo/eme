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

// TestFolderPicker_EmptyListOffersCreate: when the typed query matches no existing
// folder, the picker offers a "create new folder" row and Enter returns its resolved
// path — the fix for the dashboard's `n` dead-end where Enter did nothing.
func TestFolderPicker_EmptyListOffersCreate(t *testing.T) {
	m := NewFolderPicker([]string{"/Users/t/code/existing"})
	m.home = "/Users/t"
	m = typeInto(m, "newproj")

	if len(m.filtered) != 0 {
		t.Fatalf("filtered = %v, want empty (no existing match)", m.filtered)
	}
	if m.createPath != "/Users/t/newproj" {
		t.Fatalf("createPath = %q, want /Users/t/newproj", m.createPath)
	}
	if m.rowCount() != 1 {
		t.Fatalf("rowCount = %d, want 1 (just the create row)", m.rowCount())
	}
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(*FolderPickerModel)
	if m.Cancelled() {
		t.Error("creating a new folder must not be a cancel")
	}
	if m.Selected() != "/Users/t/newproj" {
		t.Errorf("Selected() = %q, want /Users/t/newproj", m.Selected())
	}
}

// TestFolderPicker_QueryResolution: queries resolve ~ and relative paths against home.
func TestFolderPicker_QueryResolution(t *testing.T) {
	cases := []struct{ typed, want string }{
		{"~/dev/x", "/Users/t/dev/x"},
		{"code/newproj", "/Users/t/code/newproj"},
		{"/tmp/fresh", "/tmp/fresh"},
		{"plainname", "/Users/t/plainname"},
	}
	for _, c := range cases {
		m := NewFolderPicker([]string{"/Users/t/code/existing"})
		m.home = "/Users/t"
		m = typeInto(m, c.typed)
		if m.createPath != c.want {
			t.Errorf("typed %q: createPath = %q, want %q", c.typed, m.createPath, c.want)
		}
	}
}

// TestFolderPicker_CreateOfferedEvenWithSubstringMatch: the create row stays available
// even when the typed name is a substring of an existing folder — so the user is
// never stuck with a dead end. The matched folder is still selectable above it.
func TestFolderPicker_CreateOfferedEvenWithSubstringMatch(t *testing.T) {
	m := NewFolderPicker([]string{"/Users/t/code/newproject-old"})
	m.home = "/Users/t"
	m = typeInto(m, "newproj")
	if len(m.filtered) != 1 {
		t.Fatalf("filtered = %v, want the substring-matched folder still listed", m.filtered)
	}
	if m.createPath != "/Users/t/newproj" {
		t.Errorf("createPath = %q, want /Users/t/newproj (create still offered)", m.createPath)
	}
	// the create row is the last row (after the one filtered match)
	if m.rowCount() != 2 {
		t.Errorf("rowCount = %d, want 2 (match + create row)", m.rowCount())
	}
}

// TestFolderPicker_ExactExistingNoCreate: typing the full path of an existing folder
// must not offer a duplicate create row — it is already selectable in the list.
func TestFolderPicker_ExactExistingNoCreate(t *testing.T) {
	m := NewFolderPicker([]string{"/Users/t/code/app"})
	m.home = "/Users/t"
	m = typeInto(m, "/Users/t/code/app")
	if m.createPath != "" {
		t.Errorf("createPath = %q, want empty (exact existing item)", m.createPath)
	}
}
