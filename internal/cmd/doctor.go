package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/jinmu/eme/internal/config"
	"github.com/jinmu/eme/internal/errors"
	"github.com/jinmu/eme/internal/git"
	"github.com/jinmu/eme/internal/runner"
	"github.com/jinmu/eme/internal/state"
	"github.com/jinmu/eme/internal/tmux"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor [folder]",
	Short: "Verify eme environment, or classify a candidate folder",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			return doctorFolder(args[0])
		}
		return runDoctor()
	},
}

func kindLabel(k git.Kind) string {
	switch k {
	case git.KindGreenfield:
		return "empty / greenfield"
	case git.KindNormalRoot:
		return "normal git repo (adoptable in place)"
	case git.KindNestedBare:
		return "existing eme nested-bare project"
	case git.KindLinkedWorktree:
		return "linked worktree (resolves to its main)"
	case git.KindSubmodule:
		return "git submodule (not adoptable)"
	case git.KindBareRepo:
		return "bare repository (not adoptable)"
	case git.KindSubdirectory:
		return "subdirectory of a repo (resolves to top level)"
	case git.KindBrokenGit:
		return "broken .git pointer"
	default:
		return "unknown"
	}
}

func doctorFolder(folder string) error {
	abs, err := filepath.Abs(folder)
	if err != nil {
		return err
	}
	c, err := git.Classify(abs)
	if err != nil {
		return err
	}
	fmt.Printf("folder: %s\nkind:   %s\n", abs, kindLabel(c.Kind))
	if c.Kind == git.KindNormalRoot {
		wt, derr := config.WorktreeDirFor(cfg.Worktree.DirTemplate, c.TopLevel)
		if derr == nil {
			fmt.Printf("adopt:  worktrees would go to %s\n", wt)
		}
	}
	return nil
}

func runDoctor() error {
	checks := []struct {
		name string
		fn   func() (bool, string)
	}{
		{"tmux installed", checkTmuxInstalled},
		{"tmux server reachable", checkTmuxServer},
		{"tmux popup support", checkTmuxPopup},
		{"git installed", checkGitInstalled},
		{"agent on PATH", checkAgent},
		{"state dir writable", checkStateDir},
	}

	allOK := true
	for _, c := range checks {
		ok, msg := c.fn()
		status := "ok"
		if !ok {
			status = "fail"
			allOK = false
		}
		fmt.Printf("[%s] %s: %s\n", status, c.name, msg)
	}

	if !allOK {
		return errors.New(errors.CodeCommandFailed,
			"eme doctor found problems.",
			"One or more environment checks failed.",
			"Fix the failed checks above and run `eme doctor` again.")
	}

	// Registered-project audit: non-fatal, additive.
	runRegisteredProjectAudit()

	return nil
}

// runRegisteredProjectAudit loads the persisted state and checks each in-place
// session for a missing root directory and prunable/missing worktrees.
// All findings are printed as warnings; errors never propagate to the caller.
func runRegisteredProjectAudit() {
	st, err := loadState()
	if err != nil {
		fmt.Printf("[warn] registered-project audit: could not load state: %s\n", err)
		return
	}
	if len(st.Sessions) == 0 {
		return
	}
	fmt.Println()
	fmt.Println("--- registered project audit ---")
	for i := range st.Sessions {
		sess := &st.Sessions[i]
		if sess.Layout != state.LayoutInPlace {
			continue
		}
		if _, serr := os.Stat(sess.Root); serr != nil {
			fmt.Printf("[warn] session %s: root %s missing or inaccessible: %s\n",
				sess.DisplayName, sess.Root, serr)
			continue
		}
		entries, lerr := git.WorktreeListPorcelain(sess.MainPath())
		if lerr != nil {
			fmt.Printf("[warn] session %s: could not list worktrees: %s\n",
				sess.DisplayName, lerr)
			continue
		}
		for _, wt := range entries {
			if wt.Prunable {
				fmt.Printf("[warn] session %s: worktree %s is prunable (missing working tree)\n",
					sess.DisplayName, wt.Path)
			}
		}
	}
}

func checkTmuxInstalled() (bool, string) {
	v, err := tmux.Version()
	if err != nil {
		return false, err.Error()
	}
	return true, v
}

func checkTmuxServer() (bool, string) {
	if tmux.ServerReachable() {
		return true, "server reachable"
	}
	env := tmux.DetectEnv()
	if env.SocketPath != "" {
		return false, fmt.Sprintf("server not reachable on %s", env.SocketPath)
	}
	return false, "server not running"
}

func checkTmuxPopup() (bool, string) {
	v, err := tmux.Version()
	if err != nil {
		return false, err.Error()
	}
	major, minor, ok := parseTmuxVersion(v)
	if !ok {
		return false, fmt.Sprintf("could not parse tmux version: %s", v)
	}
	if major > 3 || (major == 3 && minor >= 2) {
		return true, fmt.Sprintf("%s (popup support available)", v)
	}
	return false, fmt.Sprintf("%s (popups require tmux 3.2+)", v)
}

func parseTmuxVersion(v string) (major, minor int, ok bool) {
	var prefix string
	_, err := fmt.Sscanf(v, "%s %d.%d", &prefix, &major, &minor)
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}

func checkGitInstalled() (bool, string) {
	v, err := git.Version()
	if err != nil {
		return false, err.Error()
	}
	return true, v
}

func checkAgent() (bool, string) {
	agent := cfg.Agent.Command
	if agent == "" {
		agent = "opencode"
	}
	_, _, err := runner.Default.Run(context.Background(), "which", agent)
	if err != nil {
		return false, fmt.Sprintf("%s not found on PATH", agent)
	}
	return true, fmt.Sprintf("%s found", agent)
}

func checkStateDir() (bool, string) {
	dir := filepath.Dir(statePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err.Error()
	}
	return true, dir
}
