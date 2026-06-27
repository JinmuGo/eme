package cmd

import (
	"path/filepath"
	"testing"

	"github.com/alderwork/eme/internal/state"
)

func TestForget_RemovesFromStateOnly(t *testing.T) {
	dir := t.TempDir()
	statePath = filepath.Join(dir, "state.json")

	s := &state.State{Version: 1, Sessions: []state.Session{
		{ID: "app-1234abcd", DisplayName: "app", Root: "/p/app", Layout: state.LayoutInPlace},
	}}
	if err := saveState(s); err != nil {
		t.Fatal(err)
	}

	if err := forgetSession("app-1234abcd"); err != nil {
		t.Fatalf("forget: %v", err)
	}
	reloaded, _ := loadState()
	if reloaded.SessionByID("app-1234abcd") != nil {
		t.Errorf("session should be forgotten")
	}
}
