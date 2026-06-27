package cmd

import (
	"os"
	"testing"

	"github.com/alderwork/eme/internal/config"
	"github.com/alderwork/eme/internal/tui"
)

// TestMain gives the cmd package the production-shaped globals it gets at runtime from
// rootCmd.PersistentPreRunE, which tests that call command helpers directly never trigger:
//
//   - cfg is loaded (config.Default) so helpers that read it — e.g. resolvedAgentCommand /
//     cfg.Catalog() reached when createWorktree now onboards an agent — don't nil-panic.
//   - the agent picker defaults to a cancel (no selection) so an onboarding path never
//     blocks on the real interactive picker on a machine where agents are installed on PATH.
//
// Tests that need a concrete config or selection override cfg/pickAgent locally and restore
// these defaults on cleanup.
func TestMain(m *testing.M) {
	cfg = config.Default()
	pickAgent = func([]tui.AgentItem, string) (tui.AgentItem, bool, bool, error) {
		return tui.AgentItem{}, false, true, nil // cancelled
	}
	os.Exit(m.Run())
}
