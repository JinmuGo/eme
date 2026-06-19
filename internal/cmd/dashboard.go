package cmd

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jinmu/eme/internal/state"
	"github.com/jinmu/eme/internal/tui"
)

func runDashboard() error {
	s, err := loadReconciledState()
	if err != nil {
		return err
	}

	// If stdout is not a terminal, we cannot open an interactive TUI.
	if !isTerminal() {
		return printSessionList(s)
	}

	model := tui.NewDashboard(s.Sessions)
	if _, err := tea.NewProgram(model).Run(); err != nil {
		return fmt.Errorf("dashboard: %w", err)
	}
	return nil
}

func isTerminal() bool {
	fileInfo, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fileInfo.Mode() & os.ModeCharDevice) == os.ModeCharDevice
}

func printSessionList(s *state.State) error {
	if len(s.Sessions) == 0 {
		fmt.Println("No sessions. Run `eme new` to create one.")
		return nil
	}
	for _, sess := range s.Sessions {
		fmt.Printf("%s  %s\n", sess.ID, sess.Root)
		for _, w := range sess.Worktrees {
			fmt.Printf("  - %s (%s)\n", w.Name, w.Path)
		}
	}
	return nil
}
