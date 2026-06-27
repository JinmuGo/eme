package mcp

import "encoding/json"

func schema(s string) json.RawMessage { return json.RawMessage(s) }

const objNoArgs = `{"type":"object","properties":{},"additionalProperties":false}`

// toolDefs is the advertised tool catalog (read + non-destructive create/run).
var toolDefs = []Tool{
	{
		Name:        "list_projects",
		Description: "List all eme projects with their worktrees and each worktree's agent status (idle, working, waiting-for-input, crashed, exited).",
		InputSchema: schema(objNoArgs),
	},
	{
		Name:        "get_project",
		Description: "Get one eme project by id or display name, with its worktrees and agent status.",
		InputSchema: schema(`{"type":"object","properties":{"project":{"type":"string","description":"project id or display name"}},"required":["project"],"additionalProperties":false}`),
	},
	{
		Name:        "read_worktree_output",
		Description: "Read the recent terminal output of a worktree's agent pane (read-only; does not disturb the agent).",
		InputSchema: schema(`{"type":"object","properties":{"project":{"type":"string"},"worktree":{"type":"string","description":"worktree name; defaults to main"},"lines":{"type":"integer","description":"max trailing lines, 1-1000; defaults to 200"}},"required":["project"],"additionalProperties":false}`),
	},
	{
		Name:        "create_project",
		Description: "Create a new eme project from a local folder path. Does not start an agent unless one is requested.",
		InputSchema: schema(`{"type":"object","properties":{"folder":{"type":"string"},"agent":{"type":"string","description":"agent command to launch, or \"none\" (default) for a bare shell"}},"required":["folder"],"additionalProperties":false}`),
	},
	{
		Name:        "clone_repo",
		Description: "Clone a GitHub repo (owner/repo or URL) into a managed eme project.",
		InputSchema: schema(`{"type":"object","properties":{"repo":{"type":"string"},"agent":{"type":"string"}},"required":["repo"],"additionalProperties":false}`),
	},
	{
		Name:        "create_worktree",
		Description: "Create a new worktree (branch) in an existing eme project.",
		InputSchema: schema(`{"type":"object","properties":{"project":{"type":"string"},"name":{"type":"string"},"agent":{"type":"string"}},"required":["project","name"],"additionalProperties":false}`),
	},
	{
		Name:        "start_agent",
		Description: "Start the AI agent in a worktree (idempotent: no-op if one is already running).",
		InputSchema: schema(`{"type":"object","properties":{"project":{"type":"string"},"worktree":{"type":"string","description":"defaults to main"},"agent":{"type":"string","description":"optional agent command override"}},"required":["project"],"additionalProperties":false}`),
	},
	{
		Name:        "stop_agent",
		Description: "Stop the AI agent in a worktree by sending an interrupt. The worktree is kept (non-destructive).",
		InputSchema: schema(`{"type":"object","properties":{"project":{"type":"string"},"worktree":{"type":"string","description":"defaults to main"}},"required":["project"],"additionalProperties":false}`),
	},
}
