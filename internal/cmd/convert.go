package cmd

import "github.com/jinmu/eme/internal/errors"

// convertToNestedBare is a Phase-1 stub. A real implementation that migrates a
// normal clone into a nested-bare layout lands in a later task.
func convertToNestedBare(_ string) error {
	return errors.New(errors.CodeCommandFailed,
		"--convert is not yet implemented",
		"The conversion path lands in a later task.",
		"Run `eme new` without --convert to adopt in place.")
}
