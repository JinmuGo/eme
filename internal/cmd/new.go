package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/jinmu/eme/internal/config"
	"github.com/jinmu/eme/internal/errors"
	"github.com/jinmu/eme/internal/git"
	"github.com/jinmu/eme/internal/session"
	"github.com/jinmu/eme/internal/state"
	"github.com/jinmu/eme/internal/tmux"
	"github.com/jinmu/eme/internal/tui"
)

var (
	worktreeSession string
	newDryRun       bool
)

var newCmd = &cobra.Command{
	Use:   "new [folder]",
	Short: "Create a new project session + main worktree",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if worktreeSession != "" {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			if newDryRun {
				fmt.Printf("[dry-run] would create worktree %q in session %q\n", name, worktreeSession)
				return nil
			}
			return createWorktreePrompt(worktreeSession, name)
		}
		var folder string
		var err error
		if len(args) == 1 {
			folder = args[0]
		} else {
			folder, err = pickFolder()
			if err != nil {
				return err
			}
		}
		if newDryRun {
			fmt.Printf("[dry-run] would create project at %s\n", folder)
			return nil
		}
		return createProject(folder)
	},
}

func init() {
	newCmd.Flags().StringVar(&worktreeSession, "worktree", "", "create a worktree in an existing session")
	newCmd.Flags().BoolVar(&newDryRun, "dry-run", false, "print planned actions without executing")
}

func pickFolder() (string, error) {
	items, err := scanFolders()
	if err != nil {
		return "", fmt.Errorf("scan folders: %w", err)
	}
	picker := tui.NewFolderPicker(items)
	if _, err := tea.NewProgram(picker).Run(); err != nil {
		return "", fmt.Errorf("picker: %w", err)
	}
	if picker.Cancelled() {
		return "", nil
	}
	return picker.Selected(), nil
}

// maxScanFolders caps how many folders the picker collects, bounding work on
// large directory trees.
const maxScanFolders = 2000

// denyDirs are directory names never descended into or listed: noisy build
// artifacts and large system directories that are not project locations.
var denyDirs = map[string]bool{
	"node_modules": true,
	"Library":      true,
	"Applications": true,
	"Music":        true,
	"Movies":       true,
	"Pictures":     true,
	"Public":       true,
	"vendor":       true,
	"target":       true,
	"dist":         true,
	"build":        true,
	".cache":       true,
	"Caches":       true,
	"Pods":         true,
	"venv":         true,
	"__pycache__":  true,
}

// scanFolders gathers candidate project folders for the picker from the current
// directory, the home directory, common project roots, and any extra roots in
// config, scanning up to the configured depth.
func scanFolders() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	maxDepth := config.DefaultPickerMaxDepth
	var extraRoots []string
	if cfg != nil {
		if cfg.Picker.MaxDepth > 0 {
			maxDepth = cfg.Picker.MaxDepth
		}
		extraRoots = cfg.Picker.Roots
	}

	var roots []string
	if wd, err := os.Getwd(); err == nil {
		roots = append(roots, wd)
	}
	roots = append(roots,
		home,
		filepath.Join(home, "Projects"),
		filepath.Join(home, "code"),
		filepath.Join(home, "src"),
		filepath.Join(home, "workspace"),
		filepath.Join(home, "dev"),
		filepath.Join(home, "Development"),
	)
	for _, r := range extraRoots {
		roots = append(roots, expandTilde(r, home))
	}

	return collectFolders(roots, maxDepth), nil
}

// expandTilde expands a leading "~" in p to home.
func expandTilde(p, home string) string {
	switch {
	case p == "~":
		return home
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(home, p[2:])
	default:
		return p
	}
}

// collectFolders walks each root up to maxDepth levels deep and returns the
// deduplicated, sorted list of directories found. Hidden and denylisted
// directories are skipped, and recursion stops at project boundaries (a git
// repo or an eme ".bare" layout) so we list a project but not its internals.
func collectFolders(roots []string, maxDepth int) []string {
	seen := make(map[string]bool)
	var folders []string

	// add records a directory, returning false once the cap is reached.
	add := func(path string) bool {
		abs, err := filepath.Abs(path)
		if err != nil {
			return true
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			return true
		}
		// Resolve symlinks so two paths pointing to the same folder are not
		// listed twice.
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			resolved = abs
		}
		if seen[resolved] {
			return true
		}
		seen[resolved] = true
		folders = append(folders, abs)
		return len(folders) < maxScanFolders
	}

	var walk func(dir string, depth int) bool
	walk = func(dir string, depth int) bool {
		if depth >= maxDepth {
			return true
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return true
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.HasPrefix(name, ".") || denyDirs[name] {
				continue
			}
			child := filepath.Join(dir, name)
			if !add(child) {
				return false
			}
			// List project boundaries but do not descend into them.
			if isProjectBoundary(child) {
				continue
			}
			if !walk(child, depth+1) {
				return false
			}
		}
		return true
	}

	for _, root := range roots {
		if !walk(root, 0) {
			break
		}
	}

	sort.Strings(folders)
	return folders
}

// isProjectBoundary reports whether dir is a git repo or an eme bare layout.
func isProjectBoundary(dir string) bool {
	if git.HasGitDir(dir) {
		return true
	}
	if info, err := os.Stat(filepath.Join(dir, ".bare")); err == nil && info.IsDir() {
		return true
	}
	return false
}

func createProject(folder string) error {
	abs, err := filepath.Abs(folder)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	if info, err := os.Stat(abs); err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(abs, 0o755); err != nil {
				return fmt.Errorf("create folder: %w", err)
			}
		} else {
			return fmt.Errorf("stat folder: %w", err)
		}
	} else if !info.IsDir() {
		return errors.New(errors.CodeInvalidFolder,
			fmt.Sprintf("%s is not a directory.", abs),
			"eme projects must be folders.",
			"Provide a directory path or pick one from the picker.")
	}

	if git.HasGitDir(abs) {
		return errors.New(errors.CodeExistingGitRepo,
			fmt.Sprintf("%s already contains a git repository.", abs),
			"eme uses a nested bare layout and cannot adopt an existing git repo.",
			"Use an empty parent folder, or create the project elsewhere.")
	}

	if err := requireTmuxServer(); err != nil {
		return err
	}

	s, err := loadState()
	if err != nil {
		return err
	}

	sessID := session.ID(abs)
	if s.SessionByID(sessID) != nil {
		return errors.New(errors.CodeSessionExists,
			fmt.Sprintf("session %s is already managed by eme.", session.DisplayName(abs)),
			"The folder is already registered.",
			"Run `eme` and press Enter to switch to it.")
	}

	// Compensating transaction state.
	var createdBare bool
	var createdWorktree bool
	var createdSession bool
	var windowID string

	displayName := session.DisplayName(abs)
	// Use a clean tmux name (eme-<folder>) and only disambiguate with a numeric
	// suffix if that name is already taken by a live tmux session or another
	// eme session.
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

	sess := state.Session{
		ID:           sessID,
		DisplayName:  displayName,
		Root:         abs,
		TmuxName:     tmuxName,
		AgentCommand: cfg.Agent.Command,
	}

	cleanup := func() {
		if createdSession {
			_ = tmux.KillSession(sess.TmuxName)
		}
		if createdWorktree {
			_ = git.WorktreeRemove(filepath.Join(abs, "main"), true)
		}
		if createdBare {
			_ = os.RemoveAll(filepath.Join(abs, ".bare"))
		}
	}

	bareDir := filepath.Join(abs, ".bare")
	if err := git.InitBare(bareDir); err != nil {
		return errors.Wrap(errors.CodeCommandFailed,
			"Failed to initialize bare repository.",
			"git init --bare failed.",
			"Check that git is installed and the folder is writable.", err)
	}
	createdBare = true

	if err := git.SetDefaultBranch(bareDir, "main"); err != nil {
		cleanup()
		return errors.Wrap(errors.CodeCommandFailed,
			"Failed to set default branch.",
			"git symbolic-ref failed.",
			"Run `eme doctor` to verify git.", err)
	}

	mainWorktree := filepath.Join(abs, "main")
	if _, err := git.WorktreeAdd(bareDir, "main", "main", true); err != nil {
		cleanup()
		return errors.Wrap(errors.CodeCommandFailed,
			"Failed to create main worktree.",
			"git worktree add failed.",
			"Check git output with --verbose.", err)
	}
	createdWorktree = true

	if err := os.MkdirAll(mainWorktree, 0o755); err != nil {
		cleanup()
		return fmt.Errorf("create main worktree dir: %w", err)
	}

	if err := git.CreateEmptyCommit(mainWorktree, "main", "chore: initial commit"); err != nil {
		cleanup()
		return errors.Wrap(errors.CodeCommandFailed,
			"Failed to create initial commit.",
			"git commit --allow-empty failed.",
			"Run `eme doctor` to verify git.", err)
	}

	windowID, err = tmux.NewSession(sess.TmuxName, "main", mainWorktree)
	if err != nil {
		cleanup()
		return errors.Wrap(errors.CodeCommandFailed,
			"Failed to create tmux session.",
			"tmux new-session failed.",
			"Run `eme doctor` to verify tmux.", err)
	}
	createdSession = true

	sess.Worktrees = append(sess.Worktrees, state.Worktree{
		Name:         "main",
		Branch:       "main",
		Path:         mainWorktree,
		TmuxWindowID: windowID,
	})
	s.AddSession(sess)

	if err := saveState(s); err != nil {
		cleanup()
		return err
	}

	fmt.Printf("Created project %q at %s\n", sess.DisplayName, sess.Root)

	if tmux.DetectEnv().InsideTmux {
		_ = tmux.SwitchClient(sess.TmuxName, windowID)
	}
	return nil
}

func createWorktreePrompt(sessionArg, name string) error {
	if name == "" {
		input := tui.NewInput("Worktree name")
		if _, err := tea.NewProgram(input).Run(); err != nil {
			return fmt.Errorf("input: %w", err)
		}
		if input.Cancelled() {
			return nil
		}
		name = input.Value()
	}
	if name == "" {
		return errors.New(errors.CodeInvalidFolder,
			"Worktree name cannot be empty.",
			"No name was provided.",
			"Provide a name or type one in the prompt.")
	}
	return createWorktree(sessionArg, name)
}

func createWorktree(sessionArg, name string) error {
	if err := requireTmuxServer(); err != nil {
		return err
	}

	s, err := loadState()
	if err != nil {
		return err
	}

	sess, err := resolveSession(s, sessionArg)
	if err != nil {
		return err
	}

	if name == "main" {
		return errors.New(errors.CodeWorktreeExists,
			"'main' is reserved for the project's main worktree.",
			"Each project has exactly one main worktree.",
			"Pick a different worktree name.")
	}

	if sess.WorktreeByName(name) != nil {
		return errors.New(errors.CodeWorktreeExists,
			fmt.Sprintf("worktree %q already exists in session %q.", name, sess.DisplayName),
			"A worktree with that name is already registered.",
			"Pick a different name or remove the existing worktree first.")
	}

	mainW := sess.WorktreeByName("main")
	if mainW == nil {
		return errors.New(errors.CodeSessionNotFound,
			fmt.Sprintf("session %q has no main worktree.", sess.DisplayName),
			"The main worktree is missing or corrupted.",
			"Recreate the project with `eme new`.")
	}

	path, err := git.WorktreeAdd(mainW.Path, name, name, true)
	if err != nil {
		return errors.Wrap(errors.CodeCommandFailed,
			fmt.Sprintf("Failed to create worktree %q.", name),
			"git worktree add failed.",
			"Check git output with --verbose.", err)
	}

	windowID, err := tmux.NewWindow(sess.TmuxName, name, path)
	if err != nil {
		_ = git.WorktreeRemove(path, true)
		_ = os.RemoveAll(path)
		return errors.Wrap(errors.CodeCommandFailed,
			"Failed to create tmux window.",
			"tmux new-window failed.",
			"Check tmux output with --verbose.", err)
	}

	branch, err := git.CurrentBranch(path)
	if err != nil {
		branch = name
	}

	sess.AddWorktree(state.Worktree{
		Name:         name,
		Branch:       branch,
		Path:         path,
		TmuxWindowID: windowID,
	})

	if err := saveState(s); err != nil {
		_ = tmux.KillWindow(sess.TmuxName, windowID)
		_ = git.WorktreeRemove(path, true)
		_ = os.RemoveAll(path)
		return err
	}

	fmt.Printf("Created worktree %q in %s\n", name, sess.DisplayName)

	if tmux.DetectEnv().InsideTmux {
		_ = tmux.SwitchClient(sess.TmuxName, windowID)
	}
	return nil
}
