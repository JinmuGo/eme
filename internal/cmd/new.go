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
	convertFlag     bool
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
		if len(args) == 1 {
			folder = args[0]
		} else {
			picked, cancelled, err := pickFolder()
			if err != nil {
				return err
			}
			if cancelled {
				// The user dismissed the picker (Ctrl+C/Esc) without choosing.
				// Returning an empty path here would let createProject resolve it
				// to the current directory via filepath.Abs and adopt/switch to
				// whatever repo the cwd happens to be.
				return nil
			}
			folder = picked
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
	newCmd.Flags().BoolVar(&convertFlag, "convert", false, "restructure an existing clone into a nested-bare layout (backs up first)")
}

// pickFolder runs the interactive folder picker. cancelled is true when the
// user dismissed the picker (Ctrl+C/Esc) without selecting; callers must treat
// that as "do nothing", never as an empty-path selection (filepath.Abs("")
// resolves to the current directory).
func pickFolder() (folder string, cancelled bool, err error) {
	items, err := scanFolders()
	if err != nil {
		return "", false, fmt.Errorf("scan folders: %w", err)
	}
	picker := tui.NewFolderPicker(items)
	if _, err := tea.NewProgram(picker).Run(); err != nil {
		return "", false, fmt.Errorf("picker: %w", err)
	}
	if picker.Cancelled() {
		return "", true, nil
	}
	return picker.Selected(), false, nil
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
	// Defense in depth: an empty path resolves to the current directory via
	// filepath.Abs, which would silently adopt or switch to whatever repo the
	// cwd happens to be. A cancelled picker must never reach here, but guard
	// regardless so no future caller can trigger that jump.
	if strings.TrimSpace(folder) == "" {
		return errors.New(errors.CodeInvalidFolder,
			"No folder selected.",
			"An empty folder path resolves to the current directory.",
			"Pick a folder from the picker or run `eme new <folder>`.")
	}
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

	c, err := git.Classify(abs)
	if err != nil {
		return errors.Wrap(errors.CodeCommandFailed,
			"Failed to inspect the folder.",
			"git could not classify the directory.",
			"Run `eme doctor` to verify git.", err)
	}
	if c.Kind != git.KindGreenfield {
		return routeByClassification(c, convertFlag)
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

	cleanup := func() {
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

	if err := registerNestedBareProject(abs); err != nil {
		cleanup()
		return err
	}
	return nil
}

// registerNestedBareProject wires up a nested-bare project at root into eme's
// state and a new tmux session. It is called both from the greenfield path in
// createProject (after bare+worktree creation) and from convertToNestedBare
// (after a lossless convert). The tmux session + state write are the only side
// effects; on failure the caller is responsible for cleaning up the on-disk
// layout.
func registerNestedBareProject(root string) error {
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
	mainWorktree := filepath.Join(root, "main")
	branch, _ := git.CurrentBranch(mainWorktree)
	if branch == "" || branch == "HEAD" {
		branch = "main"
	}
	sess := state.Session{
		ID:           sessID,
		DisplayName:  displayName,
		Root:         root,
		TmuxName:     tmuxName,
		AgentCommand: cfg.Agent.Command,
		Layout:       state.LayoutNestedBare,
	}
	windowID, err := tmux.NewSession(tmuxName, "main", mainWorktree)
	if err != nil {
		return errors.Wrap(errors.CodeCommandFailed,
			"Failed to create tmux session.",
			"tmux new-session failed.",
			"Run `eme doctor` to verify tmux.", err)
	}
	sess.Worktrees = append(sess.Worktrees, state.Worktree{
		Name:         "main",
		Branch:       branch,
		Path:         mainWorktree,
		TmuxWindowID: windowID,
	})
	s.AddSession(sess)
	if err := saveState(s); err != nil {
		_ = tmux.KillSession(tmuxName) // compensating: kill the session we just made
		return err
	}
	fmt.Printf("Created project %q at %s\n", displayName, root)
	if tmux.DetectEnv().InsideTmux {
		_ = tmux.SwitchClient(tmuxName, windowID)
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

// worktreeTargetPath returns the absolute path for a new worktree named name.
func worktreeTargetPath(sess *state.Session, name string) string {
	return filepath.Join(sess.WorktreeDir(), name)
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

	if sess.WorktreeByName("main") == nil {
		return errors.New(errors.CodeSessionNotFound,
			fmt.Sprintf("session %q has no main worktree.", sess.DisplayName),
			"The main worktree is missing or corrupted.",
			"Recreate the project with `eme new`.")
	}

	target := worktreeTargetPath(sess, name)

	// Leaf-collision: refuse a pre-existing non-empty dir or file (and any
	// existing dir, for safety).
	if _, err := os.Stat(target); err == nil {
		return errors.New(errors.CodeWorktreeExists,
			fmt.Sprintf("%s already exists.", target),
			"A file or directory already occupies the worktree path.",
			"Pick a different worktree name or remove the path first.")
	}

	// Branch-collision pre-check: without this, `worktree add -b` fails loudly,
	// but a bare `<name>` would silently hijack an existing branch.
	if git.BranchExists(sess.MainPath(), name) {
		return errors.New(errors.CodeBranchExists,
			fmt.Sprintf("branch %q already exists.", name),
			"A new worktree would try to create a branch that already exists.",
			"Pick a different name, or check it out manually.")
	}

	if err := os.MkdirAll(sess.WorktreeDir(), 0o755); err != nil {
		return fmt.Errorf("create worktree dir: %w", err)
	}
	if err := git.WorktreeAddAt(sess.MainPath(), target, "", true); err != nil {
		return errors.Wrap(errors.CodeCommandFailed,
			fmt.Sprintf("Failed to create worktree %q.", name),
			"git worktree add failed.",
			"Check git output with --verbose.", err)
	}
	path := target

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

// routeByClassification handles every non-greenfield folder kind by dispatching
// to the appropriate action: adopt in-place, switch to an existing session, or
// refuse with a structured error.
func routeByClassification(c git.Classification, convert bool) error {
	switch c.Kind {
	case git.KindNormalRoot:
		if convert {
			return convertToNestedBare(c.TopLevel)
		}
		return adoptInPlace(c.TopLevel)
	case git.KindNestedBare:
		s, err := loadState()
		if err != nil {
			return err
		}
		if sess := s.SessionByRoot(c.TopLevel); sess != nil {
			return switchToSession(sess)
		}
		return adoptInPlace(c.TopLevel)
	case git.KindLinkedWorktree:
		if c.MainPath == "" {
			return errors.New(errors.CodeSessionNotFound,
				"Could not resolve the main worktree for this linked worktree.",
				"git worktree list returned no main entry.",
				"Pick the project's main folder instead.")
		}
		return adoptInPlace(c.MainPath)
	case git.KindSubdirectory:
		return adoptInPlace(c.TopLevel)
	case git.KindSubmodule:
		return errors.New(errors.CodeSubmoduleRepo,
			"That folder is a git submodule.",
			"A submodule's gitdir lives under its superproject and cannot be adopted standalone.",
			"Adopt the superproject folder instead.")
	case git.KindBareRepo:
		return errors.New(errors.CodeBareRepo,
			"That folder is a bare git repository.",
			"eme adopts working clones, not standalone bare repos.",
			"Pick a normal clone, or create a new project in an empty folder.")
	case git.KindBrokenGit:
		return errors.New(errors.CodeBrokenGit,
			"That folder has a broken .git pointer.",
			"A .git file or directory exists but git cannot use it.",
			"Repair or remove the .git pointer, then try again.")
	default:
		return errors.New(errors.CodeInvalidFolder,
			"Unrecognized folder kind.",
			"eme could not determine how to handle this folder.",
			"Pick a different folder.")
	}
}

// switchToSession switches the current tmux client to the session's main
// worktree window. If not inside tmux, it attaches to the session instead.
func switchToSession(sess *state.Session) error {
	w := sess.WorktreeByName("main")
	if w == nil {
		return errors.New(errors.CodeSessionNotFound,
			fmt.Sprintf("session %q has no main worktree.", sess.DisplayName),
			"The main worktree is missing or corrupted.",
			"Recreate the project with `eme new`.")
	}
	if tmux.DetectEnv().InsideTmux {
		if err := tmux.SwitchClient(sess.TmuxName, w.TmuxWindowID); err != nil {
			return errors.Wrap(errors.CodeCommandFailed,
				fmt.Sprintf("Could not switch to %s/main.", sess.DisplayName),
				"tmux switch-client failed.",
				"Verify the tmux session still exists with `tmux list-sessions`.", err)
		}
		return nil
	}
	if err := tmux.AttachSession(sess.TmuxName, w.TmuxWindowID); err != nil {
		return errors.Wrap(errors.CodeCommandFailed,
			fmt.Sprintf("Could not attach to %s/main.", sess.DisplayName),
			"tmux attach-session failed.",
			"Verify the tmux session exists.", err)
	}
	return nil
}
