// Package mcp implements a stdio Model Context Protocol server that exposes
// eme's non-destructive project/worktree/agent operations to external AI agents.
// It speaks JSON-RPC 2.0 over stdin/stdout and makes no network call.
package mcp

import "context"

// Worktree is the wire shape of one worktree in a project.
type Worktree struct {
	Name         string `json:"name"`
	Branch       string `json:"branch"`
	Path         string `json:"path"`
	AgentStatus  string `json:"agent_status"` // idle|working|waiting-for-input|crashed|exited
	AgentCommand string `json:"agent_command,omitempty"`
}

// Project is the wire shape of one eme project (session).
type Project struct {
	ID          string     `json:"id"`
	DisplayName string     `json:"display_name"`
	Root        string     `json:"root"`
	Layout      string     `json:"layout"`
	Worktrees   []Worktree `json:"worktrees"`
}

// AgentResult is the wire shape returned by start_agent / stop_agent.
type AgentResult struct {
	Project  string `json:"project"`
	Worktree string `json:"worktree"`
	Running  bool   `json:"running"`
	Message  string `json:"message"`
}

// Deps are the operations the server calls. internal/cmd supplies these as
// closures so the mcp package never imports cmd (avoiding an import cycle) and
// the protocol layer stays unit-testable with a fake Deps.
type Deps struct {
	ServerVersion string

	ListProjects   func(ctx context.Context) ([]Project, error)
	GetProject     func(ctx context.Context, ref string) (Project, error)
	ReadOutput     func(ctx context.Context, ref, worktree string, lines int) (string, error)
	CreateProject  func(ctx context.Context, folder, agent string) (Project, error)
	CloneRepo      func(ctx context.Context, repo, agent string) (Project, error)
	CreateWorktree func(ctx context.Context, ref, name, agent string) (Worktree, error)
	StartAgent     func(ctx context.Context, ref, worktree, agent string) (AgentResult, error)
	StopAgent      func(ctx context.Context, ref, worktree string) (AgentResult, error)
}
