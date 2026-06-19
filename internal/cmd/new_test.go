package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanFolders_DeduplicatesAndSkipsHidden(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create a visible dir and a hidden dir.
	visible := filepath.Join(home, "Projects")
	if err := os.MkdirAll(visible, 0o755); err != nil {
		t.Fatal(err)
	}
	hidden := filepath.Join(home, ".hidden")
	if err := os.MkdirAll(hidden, 0o755); err != nil {
		t.Fatal(err)
	}

	folders, err := scanFolders()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	seen := make(map[string]bool)
	for _, f := range folders {
		if seen[f] {
			t.Errorf("duplicate folder %q", f)
		}
		seen[f] = true
		if filepath.Base(f) == ".hidden" {
			t.Errorf("hidden folder should be skipped: %q", f)
		}
	}
}
