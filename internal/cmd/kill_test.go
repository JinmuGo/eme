package cmd

import (
	"testing"

	"github.com/jinmu/eme/internal/state"
)

func TestKillSession_InPlaceNeverDeletesRoot(t *testing.T) {
	sess := &state.Session{Root: "/p/app", Layout: state.LayoutInPlace}
	// pathsToDeleteForKill returns the on-disk paths a session kill would remove.
	got := pathsToDeleteForKill(sess)
	for _, p := range got {
		if p == "/p/app" {
			t.Fatalf("in-place kill must never delete the adopted clone root")
		}
	}
}

func TestKillSession_NestedBareDeletesContainer(t *testing.T) {
	sess := &state.Session{Root: "/p/app", Layout: state.LayoutNestedBare}
	got := pathsToDeleteForKill(sess)
	wantMain, wantBare := "/p/app/main", "/p/app/.bare"
	var sawMain, sawBare bool
	for _, p := range got {
		sawMain = sawMain || p == wantMain
		sawBare = sawBare || p == wantBare
	}
	if !sawMain || !sawBare {
		t.Errorf("nested-bare kill should remove main and .bare, got %v", got)
	}
}
