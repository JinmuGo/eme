package cmd

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/JinmuGo/eme/internal/gh"
	"github.com/JinmuGo/eme/internal/state"
	"github.com/JinmuGo/eme/internal/tmux"
	"github.com/JinmuGo/eme/internal/tui"
)

func runDashboard() error {
	s, err := loadReconciledState()
	if err != nil {
		return err
	}
	reconcileCaffeinate(s)

	// If stdout is not a terminal, we cannot open an interactive TUI.
	if !isTerminal() {
		return printSessionList(s)
	}

	// F1: a momentarily-down or unreachable tmux server must not block the whole
	// dashboard. Degrade to an empty snapshot — classifyStatus then reads idle/exited
	// (present=false), never a guessed running/beacon — so a user with valid state can
	// still see their worktree list. Mirrors reconcile's tolerance (commit 8f8090b).
	snap, err := tmux.PanesSnapshot()
	if err != nil {
		snap = map[string]tmux.PaneInfo{}
	}
	model := tui.NewDashboard(buildSessionViews(s.Sessions, snap, time.Now(), cfg.QuietAfterDuration()), func() ([]tui.SessionView, error) {
		rs, err := loadReconciledState()
		if err != nil {
			return nil, err
		}
		reconcileCaffeinate(rs)
		// F1 (ground truth, never a guess): if the snapshot read fails, surface the
		// error so the dashboard keeps its last-known views instead of repainting a
		// guessed status.
		snap, err := tmux.PanesSnapshot()
		if err != nil {
			return nil, err
		}
		return buildSessionViews(rs.Sessions, snap, time.Now(), cfg.QuietAfterDuration()), nil
	})
	// The auto-refresh ticker uses a cheap status-only reload: raw state (no full
	// reconcile) + the batched snapshot, skipping the per-worktree git diff. The model
	// carries the last-known diff forward between ticks (PERF-2).
	model.SetStatusReload(func() ([]tui.SessionView, error) {
		st, err := loadState()
		if err != nil {
			return nil, err
		}
		snap, err := tmux.PanesSnapshot()
		if err != nil {
			return nil, err
		}
		return buildStatusViews(st.Sessions, snap, time.Now(), cfg.QuietAfterDuration()), nil
	})
	// The `p` preview reads the FULL pane (n=0) for a persistent side panel that tails live
	// output, read-only (capture-pane). Resolved against raw state so it always targets the
	// live tmux window.
	model.SetPreview(func(sessionID, worktreeName string) ([]string, error) {
		st, err := loadState()
		if err != nil {
			return nil, err
		}
		for i := range st.Sessions {
			if st.Sessions[i].ID != sessionID {
				continue
			}
			sess := &st.Sessions[i]
			for j := range sess.Worktrees {
				if sess.Worktrees[j].Name == worktreeName {
					return tmux.CapturePane(sess.TmuxName, sess.Worktrees[j].TmuxWindowID, 0)
				}
			}
		}
		return nil, fmt.Errorf("worktree %s/%s not found", sessionID, worktreeName)
	})
	// The dashboard draws the folder/agent pickers as in-place modals (no child process takes
	// the screen). These factories keep the catalog + folder scan in cmd, so tui stays free of
	// that knowledge; the dashboard then ships the choice to a background `eme` invocation.
	model.SetFolderPicker(func() *tui.FolderPickerModel {
		items, _ := scanFolders() // a scan error just yields an empty list; the user can still type a path
		return tui.NewFolderPicker(items)
	})
	model.SetAgentPicker(func(sessionID, worktreeName string) *tui.AgentPickerModel {
		catalog := cfg.Catalog()
		items := agentItems(catalog)
		def := ""
		if st, err := loadState(); err == nil {
			if sess := st.SessionByID(sessionID); sess != nil {
				command := sess.AgentCommand
				if worktreeName != "" {
					if w := sess.WorktreeByName(worktreeName); w != nil {
						command = resolvedAgentCommand(sess, w)
					}
				}
				def = defaultAgentName(catalog, command)
			}
		}
		return tui.NewAgentPicker(items, def)
	})
	// The clone picker's repo list is network-bound (gh repo list), so the dashboard fetches it
	// asynchronously behind a loading modal. cmd owns the gh call + the gh.Repo→tui.RepoItem
	// mapping, keeping tui free of gh.
	model.SetRepoFetcher(func() ([]tui.RepoItem, error) {
		if !gh.Available() {
			return nil, errGhNotFound()
		}
		if !gh.Authed(context.Background()) {
			return nil, errGhNotAuthed()
		}
		repos, err := gh.RepoList(context.Background(), 200)
		if err != nil {
			return nil, err
		}
		items := make([]tui.RepoItem, len(repos))
		for i, r := range repos {
			items[i] = tui.RepoItem{NameWithOwner: r.NameWithOwner, Description: r.Description, Private: r.IsPrivate}
		}
		return items, nil
	})
	finalModel, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
	if err != nil {
		return fmt.Errorf("dashboard: %w", err)
	}
	return switchFromModel(finalModel)
}

// switchFromModel execs `eme switch` if the dashboard recorded a switch target
// (Enter), otherwise returns nil. It runs after bubbletea has restored the
// terminal. Split out from runDashboard so the cross-package handoff is testable
// without a TTY.
func switchFromModel(finalModel tea.Model) error {
	dm, ok := finalModel.(*tui.DashboardModel)
	if !ok {
		return nil
	}
	session, worktree, ok := dm.SwitchTarget()
	if !ok {
		return nil
	}
	return execSwitch(session, worktree)
}

// execReplace replaces the current process image. It is a package var so tests
// can capture the argv without actually exec'ing.
var execReplace = syscall.Exec

// execSwitch replaces the current process with `eme switch <session> [worktree]`.
// It runs only after the dashboard's bubbletea program has exited and restored
// the terminal, so the handoff happens on a clean terminal.
func execSwitch(session, worktree string) error {
	binary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate eme binary: %w", err)
	}
	argv := []string{"eme", "switch", session}
	if worktree != "" {
		argv = append(argv, worktree)
	}
	return execReplace(binary, argv, os.Environ())
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
