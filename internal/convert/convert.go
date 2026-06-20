// Package convert restructures a normal clone into the nested-bare layout
// losslessly (cp -al the gitdir; original stays read-only) with atomic swap.
package convert

import (
	"context"
	"strings"

	"github.com/jinmu/eme/internal/errors"
	"github.com/jinmu/eme/internal/git"
)

type Options struct {
	Stash       bool
	NoUntracked bool
}

// CheckPreconditions verifies root can be converted safely.
func CheckPreconditions(root string, opts Options) error {
	out, _, err := git.Run(context.Background(), root, "status", "--porcelain")
	if err != nil {
		return errors.Wrap(errors.CodeCommandFailed, "Failed to read git status.",
			"git status --porcelain failed.", "Run `eme doctor` to verify git.", err)
	}
	if strings.TrimSpace(out) != "" && !opts.Stash {
		return errors.New(errors.CodeDirtyTree,
			"The repository has uncommitted or untracked changes.",
			"Convert needs a clean working tree to swap layouts safely.",
			"Commit or stash your changes, or pass --stash.")
	}
	mods, _, _ := git.Run(context.Background(), root, "ls-files", "--", ".gitmodules")
	if strings.TrimSpace(mods) != "" {
		return errors.New(errors.CodeSubmodulesUnsupported,
			"This repository uses submodules.",
			"Converting submodules safely is not supported yet (relative gitdir paths break on the move).",
			"Adopt it in place instead (run `eme new` without --convert).")
	}
	return nil
}
