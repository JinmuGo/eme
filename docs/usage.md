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
5. Press `a` to launch your AI agent.

## CLI commands

```text
eme                # dashboard
eme new [folder]   # create project + main worktree
eme new --worktree <session> [name]  # create worktree
eme switch <session> [worktree]      # switch window
eme kill <session> [worktree]        # remove (needs --force)
eme agent <session> [worktree]       # toggle agent
eme doctor         # verify environment
eme --version      # print version
```

## Configuration

`~/.config/eme/config.toml`:

```toml
[agent]
command = "opencode"
```

You can override the agent per folder or per worktree from the dashboard.

## Dashboard keys

| Key | Action |
|-----|--------|
| `n` | New project |
| `c` | Create worktree in selected project |
| `Enter` / `o` | Switch to selected project |
| `d` | Kill selected project |
| `a` | Toggle agent in selected project |
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
