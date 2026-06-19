package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_MissingFileReturnsEmptyState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.json")
	s, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Version != Version {
		t.Errorf("expected version %d, got %d", Version, s.Version)
	}
	if len(s.Sessions) != 0 {
		t.Errorf("expected empty sessions, got %d", len(s.Sessions))
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := &State{Sessions: []Session{
		{ID: "foo", DisplayName: "foo", Root: "/tmp/foo", TmuxName: "foo"},
	}}
	if err := s.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Sessions) != 1 || loaded.Sessions[0].ID != "foo" {
		t.Errorf("loaded state mismatch: %+v", loaded.Sessions)
	}
}

func TestSave_Atomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := &State{Sessions: []Session{{ID: "foo"}}}
	if err := s.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	// There should be no leftover temp file.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("expected no temp file after atomic save")
	}
}

func TestSessionByRoot(t *testing.T) {
	s := &State{Sessions: []Session{
		{ID: "a", Root: "/tmp/a"},
		{ID: "b", Root: "/tmp/b"},
	}}
	got := s.SessionByRoot("/tmp/b")
	if got == nil || got.ID != "b" {
		t.Errorf("expected session b, got %+v", got)
	}
}
