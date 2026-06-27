package cmd

import (
	"context"
	"strings"

	"github.com/alderwork/eme/internal/mcp"
	"github.com/alderwork/eme/internal/state"
	"github.com/alderwork/eme/internal/tmux"
	"github.com/alderwork/eme/internal/tui"
)

// agentStatusString maps the internal AgentStatus to the MCP wire string.
func agentStatusString(s tui.AgentStatus) string {
	switch s {
	case tui.StatusWorking:
		return "working"
	case tui.StatusWaiting:
		return "waiting-for-input"
	case tui.StatusCrashed:
		return "crashed"
	case tui.StatusExited:
		return "exited"
	default:
		return "idle"
	}
}

func mcpWorktree(sess *state.Session, w *state.Worktree, snap map[string]tmux.PaneInfo) mcp.Worktree {
	info, present := snap[w.TmuxWindowID]
	return mcp.Worktree{
		Name:         w.Name,
		Branch:       w.Branch,
		Path:         w.Path,
		AgentStatus:  agentStatusString(classifyStatus(info, present, w.LastAgentCommand)),
		AgentCommand: w.LastAgentCommand,
	}
}

func toMCPProject(sess *state.Session, snap map[string]tmux.PaneInfo) mcp.Project {
	p := mcp.Project{
		ID:          sess.ID,
		DisplayName: sess.DisplayName,
		Root:        sess.Root,
		Layout:      sess.Layout,
	}
	for j := range sess.Worktrees {
		p.Worktrees = append(p.Worktrees, mcpWorktree(sess, &sess.Worktrees[j], snap))
	}
	return p
}

func mcpListProjects(ctx context.Context) ([]mcp.Project, error) {
	s, err := loadReconciledState()
	if err != nil {
		return nil, err
	}
	snap, _ := tmux.PanesSnapshot() // best-effort: nil map degrades to idle/exited
	projects := make([]mcp.Project, 0, len(s.Sessions))
	for i := range s.Sessions {
		projects = append(projects, toMCPProject(&s.Sessions[i], snap))
	}
	return projects, nil
}

func mcpGetProject(ctx context.Context, ref string) (mcp.Project, error) {
	s, err := loadReconciledState()
	if err != nil {
		return mcp.Project{}, err
	}
	sess, err := resolveSession(s, ref)
	if err != nil {
		return mcp.Project{}, err
	}
	snap, _ := tmux.PanesSnapshot()
	return toMCPProject(sess, snap), nil
}

func mcpReadOutput(ctx context.Context, ref, worktree string, lines int) (string, error) {
	if worktree == "" {
		worktree = "main"
	}
	if lines <= 0 {
		lines = 200
	}
	if lines > 1000 {
		lines = 1000
	}
	s, err := loadState()
	if err != nil {
		return "", err
	}
	sess, err := resolveSession(s, ref)
	if err != nil {
		return "", err
	}
	w, err := resolveWorktree(sess, worktree)
	if err != nil {
		return "", err
	}
	out, err := tmux.CapturePane(sess.TmuxName, w.TmuxWindowID, lines)
	if err != nil {
		return "", err
	}
	return strings.Join(out, "\n"), nil
}
