package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/muesli/termenv"
	"github.com/spf13/cobra"

	"github.com/JinmuGo/eme/internal/config"
	"github.com/JinmuGo/eme/internal/errors"
	"github.com/JinmuGo/eme/internal/gh"
	"github.com/JinmuGo/eme/internal/git"
	"github.com/JinmuGo/eme/internal/runner"
	"github.com/JinmuGo/eme/internal/state"
	"github.com/JinmuGo/eme/internal/tmux"
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
	switch c.Kind {
	case git.KindNormalRoot:
		wt, derr := config.WorktreeDirFor(cfg.Worktree.DirTemplate, c.TopLevel)
		if derr == nil {
			fmt.Printf("adopt:  worktrees would go to %s\n", wt)
		}
	case git.KindGreenfield:
		// For a non-git folder, eme's action depends on whether it already has
		// content: an empty folder is scaffolded into a new project, a folder
		// with content is adopted in place as a plain (non-git) project.
		if empty, eerr := dirIsEffectivelyEmpty(abs); eerr == nil {
			if empty {
				fmt.Printf("action: empty → scaffold a new nested-bare project (.bare + main/)\n")
			} else {
				fmt.Printf("action: has content → adopt in place as a plain folder (agent runs here; no git worktrees)\n")
			}
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
		{"tmux socket", checkTmuxSocket},
		{"tmux server reachable", checkTmuxServer},
		{"tmux popup support", checkTmuxPopup},
		{"terminal color", checkColor},
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

	// gh is informational: required for `eme clone`, optional for core eme, so a
	// missing/unauthed gh never fails doctor.
	ghOK, ghMsg := checkGh()
	ghStatus := "info"
	if !ghOK {
		ghStatus = "warn"
	}
	fmt.Printf("[%s] gh CLI: %s\n", ghStatus, ghMsg)

	// Registered-project audit: non-fatal, additive.
	runRegisteredProjectAudit()

	return nil
}

// checkGh reports gh availability/auth. It is informational: eme clone needs gh,
// but core eme does not, so a missing gh never fails `eme doctor`.
func checkGh() (bool, string) {
	if !gh.Available() {
		return false, "not installed (needed for `eme clone`; see https://cli.github.com)"
	}
	if !gh.Authed(context.Background()) {
		return false, "installed but not authenticated (run `gh auth login` for `eme clone`)"
	}
	return true, "installed and authenticated"
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

// checkTmuxSocket reports which tmux server eme is pinned to. It is informational
// (always ok) but directly diagnoses "my sessions differ between shell and popup"
// by making the single managed server visible.
func checkTmuxSocket() (bool, string) {
	if tmux.Socket == "" {
		return true, "ambient (follows $TMUX)"
	}
	return true, fmt.Sprintf("%s (%s)", tmux.Socket, tmux.ManagedSocketPath())
}

func checkTmuxServer() (bool, string) {
	if tmux.ServerReachable() {
		return true, "server reachable"
	}
	if sock := tmux.ManagedSocketPath(); sock != "" {
		return false, fmt.Sprintf("server not reachable on %s", sock)
	}
	if env := tmux.DetectEnv(); env.SocketPath != "" {
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

// checkColor reports the terminal color depth the dashboard will render at. It is
// always "ok" — color is enhancement, never a requirement (every status also carries a
// glyph + label), so degraded color is a heads-up, not a failure. The actionable case is
// a tmux user without truecolor: the dashboard popup then renders the steel-blue/amber in
// eme's pinned 256/16-color fallbacks instead of the exact theme hexes, so it points at
// the one-line terminal-features fix that lets 24-bit reach the popup.
func checkColor() (bool, string) {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return true, "NO_COLOR set — monochrome by request (status reads by glyph + label)"
	}
	return true, colorProfileMessage(termenv.ColorProfile(), os.Getenv("TMUX") != "")
}

// colorProfileMessage describes what a detected color profile means for the dashboard.
// Split from checkColor so the wording (and the tmux truecolor nudge) is unit-testable
// without a live terminal — termenv.ColorProfile() reports Ascii whenever stdout is not a
// TTY, so the real detection only happens on an interactive run.
func colorProfileMessage(p termenv.Profile, inTmux bool) string {
	switch p {
	case termenv.TrueColor:
		return "truecolor (24-bit) — full theme palette"
	case termenv.ANSI256:
		msg := "256-color — eme uses its pinned fallbacks (amber/blue/orange preserved)"
		if inTmux {
			msg += "; add " + "`set -ga terminal-features \",*:RGB\"`" + " to ~/.tmux.conf for 24-bit in popups"
		}
		return msg
	case termenv.ANSI:
		return "16-color — basic ANSI hues (each status still distinct)"
	default:
		return "no color detected — status reads by glyph + label"
	}
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
