package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/alderwork/eme/internal/errors"
	"github.com/alderwork/eme/internal/mcp"
	"github.com/alderwork/eme/internal/session"
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

func mcpWorktree(w *state.Worktree, snap map[string]tmux.PaneInfo) mcp.Worktree {
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
		p.Worktrees = append(p.Worktrees, mcpWorktree(&sess.Worktrees[j], snap))
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
	snap, _ := tmux.PanesSnapshot() // best-effort: nil map degrades to idle/exited
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

// runEme re-execs the eme binary with the server's resolved state/config paths
// and the pinned tmux socket, so write tools reuse the exact, battle-tested CLI
// code path. It is a package var so tests can substitute a fake.
var runEme = func(args ...string) (string, string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", "", err
	}
	full := append([]string{"--state", statePath, "--config", configPath}, args...)
	c := exec.Command(exe, full...)
	c.Env = append(os.Environ(), "EME_TMUX_SOCKET="+tmux.Socket)
	var stdout, stderr bytes.Buffer
	c.Stdout, c.Stderr = &stdout, &stderr
	err = c.Run()
	return stdout.String(), stderr.String(), err
}

// mcpExecErr turns a failed re-exec into a clean error carrying eme's own
// what/why/how message (eme prints it to stderr prefixed with "eme: ").
func mcpExecErr(stderr string, err error) error {
	msg := strings.TrimSpace(stderr)
	msg = strings.TrimPrefix(msg, "eme: ")
	if msg == "" {
		return err
	}
	return fmt.Errorf("%s", msg)
}

func sessionIDSet(s *state.State) map[string]bool {
	m := make(map[string]bool, len(s.Sessions))
	for i := range s.Sessions {
		m[s.Sessions[i].ID] = true
	}
	return m
}

// newSession returns the first session in s whose id was not present in before.
func newSession(s *state.State, before map[string]bool) *state.Session {
	for i := range s.Sessions {
		if !before[s.Sessions[i].ID] {
			return &s.Sessions[i]
		}
	}
	return nil
}

func mcpCreateProject(ctx context.Context, folder, agent string) (mcp.Project, error) {
	if agent == "" {
		agent = "none"
	}
	abs, err := filepath.Abs(folder)
	if err != nil {
		return mcp.Project{}, err
	}
	before, err := loadState()
	if err != nil {
		return mcp.Project{}, err
	}
	beforeIDs := sessionIDSet(before)
	if _, stderr, err := runEme("new", abs, "--no-switch", "--agent", agent); err != nil {
		return mcp.Project{}, mcpExecErr(stderr, err)
	}
	after, err := loadState()
	if err != nil {
		return mcp.Project{}, err
	}
	sess := newSession(after, beforeIDs)
	if sess == nil {
		sess = after.SessionByID(session.ID(abs))
	}
	if sess == nil {
		sess = after.SessionByRoot(abs)
	}
	if sess == nil {
		return mcp.Project{}, fmt.Errorf("project created but not found in state for %s", abs)
	}
	snap, _ := tmux.PanesSnapshot()
	return toMCPProject(sess, snap), nil
}

func mcpCloneRepo(ctx context.Context, repo, agent string) (mcp.Project, error) {
	if agent == "" {
		agent = "none"
	}
	before, err := loadState()
	if err != nil {
		return mcp.Project{}, err
	}
	beforeIDs := sessionIDSet(before)
	if _, stderr, err := runEme("clone", repo, "--no-switch", "--agent", agent); err != nil {
		return mcp.Project{}, mcpExecErr(stderr, err)
	}
	after, err := loadState()
	if err != nil {
		return mcp.Project{}, err
	}
	sess := newSession(after, beforeIDs)
	if sess == nil {
		return mcp.Project{}, fmt.Errorf("cloned %s but no new project appeared in state", repo)
	}
	snap, _ := tmux.PanesSnapshot()
	return toMCPProject(sess, snap), nil
}

func mcpStartAgent(ctx context.Context, ref, worktree, agent string) (mcp.AgentResult, error) {
	if worktree == "" {
		worktree = "main"
	}
	s, err := loadState()
	if err != nil {
		return mcp.AgentResult{}, err
	}
	sess, err := resolveSession(s, ref)
	if err != nil {
		return mcp.AgentResult{}, err
	}
	w, err := resolveWorktree(sess, worktree)
	if err != nil {
		return mcp.AgentResult{}, err
	}
	running, err := agentRunningFn(w)
	if err != nil {
		return mcp.AgentResult{}, err
	}
	res := mcp.AgentResult{Project: sess.DisplayName, Worktree: w.Name}
	if running {
		res.Running = true
		res.Message = "agent already running"
		return res, nil
	}
	if agent != "" && agent != "none" {
		if _, stderr, err := runEme("agent", sess.ID, w.Name, "--set", agent); err != nil {
			return mcp.AgentResult{}, mcpExecErr(stderr, err)
		}
	} else {
		if _, stderr, err := runEme("agent", sess.ID, w.Name); err != nil {
			return mcp.AgentResult{}, mcpExecErr(stderr, err)
		}
	}
	res.Running = true
	res.Message = "agent started"
	return res, nil
}

func mcpStopAgent(ctx context.Context, ref, worktree string) (mcp.AgentResult, error) {
	if worktree == "" {
		worktree = "main"
	}
	s, err := loadState()
	if err != nil {
		return mcp.AgentResult{}, err
	}
	sess, err := resolveSession(s, ref)
	if err != nil {
		return mcp.AgentResult{}, err
	}
	w, err := resolveWorktree(sess, worktree)
	if err != nil {
		return mcp.AgentResult{}, err
	}
	running, err := agentRunningFn(w)
	if err != nil {
		return mcp.AgentResult{}, err
	}
	res := mcp.AgentResult{Project: sess.DisplayName, Worktree: w.Name}
	if !running {
		res.Message = "no agent running"
		return res, nil
	}
	if _, stderr, err := runEme("agent", sess.ID, w.Name); err != nil {
		return mcp.AgentResult{}, mcpExecErr(stderr, err)
	}
	res.Message = "sent interrupt to agent"
	return res, nil
}

func mcpCreateWorktree(ctx context.Context, ref, name, agent string) (mcp.Worktree, error) {
	if agent == "" {
		agent = "none"
	}
	if _, stderr, err := runEme("new", "--worktree", ref, name, "--no-switch", "--agent", agent); err != nil {
		return mcp.Worktree{}, mcpExecErr(stderr, err)
	}
	s, err := loadState()
	if err != nil {
		return mcp.Worktree{}, err
	}
	sess, err := resolveSession(s, ref)
	if err != nil {
		return mcp.Worktree{}, err
	}
	w := sess.WorktreeByName(name)
	if w == nil {
		return mcp.Worktree{}, fmt.Errorf("worktree %q created but not found in state", name)
	}
	snap, _ := tmux.PanesSnapshot()
	return mcpWorktree(w, snap), nil
}

func newMCPDeps() mcp.Deps {
	return mcp.Deps{
		ServerVersion:  Version,
		ListProjects:   mcpListProjects,
		GetProject:     mcpGetProject,
		ReadOutput:     mcpReadOutput,
		CreateProject:  mcpCreateProject,
		CloneRepo:      mcpCloneRepo,
		CreateWorktree: mcpCreateWorktree,
		StartAgent:     mcpStartAgent,
		StopAgent:      mcpStopAgent,
	}
}

// ensureMCPServer guarantees the pinned tmux server is reachable. Because the MCP
// server runs outside tmux (as a subprocess of the AI client), it manages its own
// dedicated server; a holder session is started detached when none exists.
func ensureMCPServer() error {
	if _, err := tmux.Version(); err != nil {
		return errors.New(errors.CodeTmuxNotFound,
			"tmux is not installed or not on PATH.",
			"eme requires tmux to manage sessions.",
			"Install tmux and make sure it is available on PATH.")
	}
	if tmux.ServerReachable() {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	if _, err := tmux.NewSession("eme", "eme", home); err != nil {
		return errors.Wrap(errors.CodeCommandFailed,
			"Failed to start the eme tmux server.",
			"tmux new-session failed.",
			"Verify tmux works with `eme doctor`.", err)
	}
	return nil
}

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run an MCP server so external AI agents can manage eme projects",
	Long: `Run a Model Context Protocol (MCP) server on stdin/stdout so an external AI
agent (Claude Desktop, Claude Code, Cursor, …) can drive eme as a tool. It speaks
JSON-RPC 2.0 over stdio and makes no network call.

Add it to an MCP client, e.g. Claude Code:

    claude mcp add eme -- eme mcp

Tools (read): list_projects, get_project, read_worktree_output
Tools (create/run, non-destructive): create_project, clone_repo, create_worktree,
start_agent, stop_agent

Destructive operations (kill, clean, forget) are intentionally NOT exposed; run
them yourself. The server manages its own dedicated tmux server (socket "eme" by
default, or EME_TMUX_SOCKET / config); attach with: tmux -L eme attach`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if tmux.Socket == "" {
			tmux.Socket = "eme"
		}
		if err := ensureMCPServer(); err != nil {
			return err
		}
		return mcp.Serve(cmd.InOrStdin(), cmd.OutOrStdout(), newMCPDeps())
	},
}
