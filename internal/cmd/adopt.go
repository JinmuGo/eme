package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jinmu/eme/internal/config"
	"github.com/jinmu/eme/internal/errors"
	"github.com/jinmu/eme/internal/git"
	"github.com/jinmu/eme/internal/session"
	"github.com/jinmu/eme/internal/state"
	"github.com/jinmu/eme/internal/tmux"
)

// adoptInPlace registers a normal repo root as an in-place eme project: the
// clone is the main worktree; new worktrees go to a sibling container.
func adoptInPlace(root string) error {
	if err := requireTmuxServer(); err != nil {
		return err
	}
	s, err := loadState()
	if err != nil {
		return err
	}

	sessID := session.ID(root)
	if s.SessionByID(sessID) != nil {
		return errors.New(errors.CodeSessionExists,
			fmt.Sprintf("session %s is already managed by eme.", session.DisplayName(root)),
			"The folder is already registered.",
			"Run `eme` and press Enter to switch to it.")
	}

	worktreeDir, err := config.WorktreeDirFor(cfg.Worktree.DirTemplate, root)
	if err != nil {
		return errors.Wrap(errors.CodeConfigInvalid,
			"Invalid worktree dir_template.",
			err.Error(),
			"Fix [worktree] dir_template in your config.", err)
	}

	branch, err := git.CurrentBranch(root)
	if err != nil || branch == "HEAD" {
		branch = "" // detached: leave empty; window label falls back to short SHA elsewhere.
	}

	displayName := session.DisplayName(root)
	tmuxName := session.UniqueTmuxName(displayName, func(name string) bool {
		if tmux.SessionExists(name) {
			return true
		}
		for i := range s.Sessions {
			if s.Sessions[i].TmuxName == name {
				return true
			}
		}
		return false
	})

	windowID, err := tmux.NewSession(tmuxName, "main", root)
	if err != nil {
		return errors.Wrap(errors.CodeCommandFailed,
			"Failed to create tmux session.",
			"tmux new-session failed.",
			"Run `eme doctor` to verify tmux.", err)
	}

	var override string
	if cfg.Worktree.DirTemplate != config.Default().Worktree.DirTemplate {
		override = worktreeDir
	}
	sess := state.Session{
		ID:                  sessID,
		DisplayName:         displayName,
		Root:                root,
		Layout:              state.LayoutInPlace,
		WorktreeDirOverride: override,
		TmuxName:            tmuxName,
		AgentCommand:        cfg.Agent.Command,
		Worktrees: []state.Worktree{{
			Name:         "main",
			Branch:       branch,
			Path:         root,
			TmuxWindowID: windowID,
		}},
	}
	s.AddSession(sess)
	if err := saveState(s); err != nil {
		_ = tmux.KillSession(tmuxName)
		return err
	}

	// Keep the sibling worktree container out of a parent repo's git status.
	_ = excludeFromParentRepo(worktreeDir)

	fmt.Printf("Adopted %q in place at %s\n", displayName, root)
	if stored := s.SessionByID(sessID); stored != nil {
		maybeOnboardAgent(s, stored)
	}
	maybeSwitchClient(tmuxName, windowID)
	return nil
}

// excludeFromParentRepo appends worktreeDir's basename to the enclosing repo's
// .git/info/exclude (local, never committed) when worktreeDir's parent is itself
// a git work tree. No-op otherwise.
func excludeFromParentRepo(worktreeDir string) error {
	parent := filepath.Dir(worktreeDir)
	infoDir := filepath.Join(parent, ".git", "info")
	if fi, err := os.Stat(infoDir); err != nil || !fi.IsDir() {
		return nil
	}
	entry := filepath.Base(worktreeDir) + "/"
	excludePath := filepath.Join(infoDir, "exclude")
	if data, err := os.ReadFile(excludePath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) == entry {
				return nil // already excluded
			}
		}
	}
	f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "# added by eme\n%s\n", entry)
	return err
}
