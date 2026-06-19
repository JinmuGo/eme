package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jinmu/eme/internal/config"
	"github.com/jinmu/eme/internal/errors"
	"github.com/jinmu/eme/internal/reconcile"
	"github.com/jinmu/eme/internal/runner"
	"github.com/jinmu/eme/internal/state"
	"github.com/jinmu/eme/internal/tmux"
)

var (
	// Version is set at build time with -ldflags.
	Version = "dev"

	statePath  = state.DefaultPath()
	configPath = config.DefaultPath()
	cfg        *config.Config
)

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

var rootCmd = &cobra.Command{
	Use:   "eme",
	Short: "AI agent session manager for git worktrees",
	Long: `eme manages git worktrees as tmux windows.
Each project gets a tmux session; each worktree gets a window.
Run inside a tmux popup for the best experience.`,
	Version: Version,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		cfg, err = config.Load(configPath)
		if err != nil {
			return err
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDashboard()
	},
}

func init() {
	// eme prints errors itself (see cmd/eme/main.go), with a single "eme:"
	// prefix. Silence cobra's own error + usage dump so errors are not printed
	// twice and runtime failures don't spew the full help text.
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true

	rootCmd.SetVersionTemplate("eme {{.Version}}\n")
	rootCmd.PersistentFlags().StringVar(&statePath, "state", statePath, "path to state file")
	rootCmd.PersistentFlags().StringVar(&configPath, "config", configPath, "path to config file")
	rootCmd.PersistentFlags().BoolVar(&runner.Verbose, "verbose", false, "print external commands to stderr")

	rootCmd.AddCommand(newCmd)
	rootCmd.AddCommand(switchCmd)
	rootCmd.AddCommand(killCmd)
	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(versionCmd)
}

func loadState() (*state.State, error) {
	s, err := state.Load(statePath)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func loadReconciledState() (*state.State, error) {
	s, err := loadState()
	if err != nil {
		return nil, err
	}
	if reconcile.State(s) {
		if err := saveState(s); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func saveState(s *state.State) error {
	return s.Save(statePath)
}

func requireTmuxServer() error {
	if _, err := tmux.Version(); err != nil {
		return errors.New(errors.CodeTmuxNotFound,
			"tmux is not installed or not on PATH.",
			"eme requires tmux to manage sessions.",
			"Install tmux and make sure it is available on PATH.")
	}
	if !tmux.ServerReachable() {
		return errors.New(errors.CodeTmuxServerMissing,
			"tmux server is not running.",
			"No tmux server is reachable on the current socket.",
			"Start it with: tmux new-session -d")
	}
	return nil
}

// resolveSession finds a session by id or unambiguous display name.
func resolveSession(s *state.State, name string) (*state.Session, error) {
	if sess := s.SessionByID(name); sess != nil {
		return sess, nil
	}
	var matches []*state.Session
	for i := range s.Sessions {
		if s.Sessions[i].DisplayName == name || strings.HasSuffix(s.Sessions[i].ID, "-"+name) {
			matches = append(matches, &s.Sessions[i])
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		ids := make([]string, len(matches))
		for i, m := range matches {
			ids[i] = m.ID
		}
		return nil, errors.New(errors.CodeSessionNotFound,
			fmt.Sprintf("session name %q is ambiguous.", name),
			"Multiple sessions share that display name.",
			fmt.Sprintf("Use the full session id. Candidates: %s", strings.Join(ids, ", ")))
	}
	return nil, errors.New(errors.CodeSessionNotFound,
		fmt.Sprintf("session %q not found.", name),
		"No session matches the given id or display name.",
		"Run `eme` to open the dashboard and see available sessions.")
}

// resolveWorktree finds a worktree in a session by name.
func resolveWorktree(sess *state.Session, name string) (*state.Worktree, error) {
	if w := sess.WorktreeByName(name); w != nil {
		return w, nil
	}
	return nil, errors.New(errors.CodeSessionNotFound,
		fmt.Sprintf("worktree %q not found in session %q.", name, sess.DisplayName),
		"The worktree has not been created or has been removed.",
		"Run `eme` and use `c` to create a new worktree.")
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print eme version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("eme", Version)
	},
}
