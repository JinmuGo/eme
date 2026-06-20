package cmd

import (
	"fmt"

	"github.com/jinmu/eme/internal/convert"
)

// convertToNestedBare converts an existing clone at root into nested-bare, then
// registers it as a greenfield-style session (Layout nested-bare).
func convertToNestedBare(root string) error {
	if newDryRun {
		fmt.Printf("[dry-run] would convert %s into a nested-bare layout (with backup)\n", root)
		return nil
	}
	backup, err := convert.Convert(root, convert.Options{})
	if err != nil {
		return err
	}
	fmt.Printf("Converted %s. Backup kept at %s — delete it once you've verified.\n", root, backup)
	// After conversion <root> is a nested-bare container; reuse the greenfield
	// registration path against the now-existing .bare + main worktree.
	return registerNestedBareProject(root)
}
