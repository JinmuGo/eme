# eme Usage Guide

## Overview

`eme` maps a project folder to a tmux session and each git worktree to a tmux window. AI agents run inside those windows.

## Install

```bash
go install github.com/jinmu/eme@latest
```

## Bind tmux

Add to `~/.tmux.conf`:

```tmux
bind-key a display-popup -E -w 80% -h 80% eme
```

Reload with `tmux source-file ~/.tmux.conf`.

## Your first project

1. Press `<prefix> a` to open the dashboard.
2. Press `n` and pick a folder.
3. `eme` creates `<folder>/main`, a tmux session, and a window.
4. Press `c` and type a worktree name.
5. Press `a` to launch your AI agent (or `A` to pick which one).

## CLI commands

```text
eme                # dashboard
eme new [folder]   # create project + main worktree
eme new --worktree <session> [name]  # create worktree
eme switch <session> [worktree]      # switch window
eme kill <session> [worktree]        # remove (needs --force)
eme agent <session> [worktree]       # toggle agent
eme agent <session> [worktree] --pick # choose the worktree's agent
eme doctor         # verify environment
eme --version      # print version
```

## Configuration

`~/.config/eme/config.toml`:

```toml
[agent]
command = "opencode"

[[agents]]
name = "claude-resume"
command = "claude --resume"
```

You can override the agent per folder or per worktree from the dashboard.

## Dashboard keys

The tree uses vim/nvim-style motions — sessions fold like a file tree.

| Key | Action |
|-----|--------|
| `↓` / `j`, `↑` / `k` | Move down / up (over session headers and worktrees) |
| `→` / `l` | Expand a folded session, step into a session, or open a worktree |
| `←` / `h` | Fold a session (from a worktree, fold its parent and jump to the header) |
| `Enter` / `o` | On a worktree: switch to it · On a session header: toggle fold |
| `p` | Peek the selected worktree's last pane lines (read-only) |
| `n` | New project |
| `c` | Create worktree in the session under the cursor |
| `d` | Kill the worktree, or the whole project on a `main`/header row |
| `a` | Toggle agent in the selected worktree |
| `A` | Pick the selected worktree's agent from the catalog |
| `x` | Reset a crashed/exited worktree's pane back to idle |
| `?` | Toggle help |
| `q` / `Esc` | Quit |

## Worktree layout

`eme` creates a nested bare repository:

```text
<folder>/
  .bare/       # bare git repo
  main/        # main worktree
  feature/     # additional worktree
```

Do not use a folder that already contains a `.git` repository.

## Troubleshooting

- **tmux server is not running**: start one with `tmux new-session -d`.
- **Agent not found**: install the agent or set `agent.command`.
- **Session name ambiguous**: use the full session id shown in `eme`.
