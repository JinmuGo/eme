# eme

> Eeny, meeny, miny, moe.

**Mission control for your AI coding agents.** Which agent needs you? eme counts them out.

`eme` runs each AI coding agent in its own git worktree as a real tmux session, and shows you — across every worktree — which one is `waiting-for-input`, which is still `working`, and which has `exited`. All without leaving the terminal.

![demo](docs/demo.gif)

## Install

```bash
go install github.com/jinmu/eme@latest
```

Make sure `$GOPATH/bin` is on your `PATH`.

## Bind it to tmux

Add to `~/.tmux.conf`:

```tmux
bind-key a display-popup -E -w 80% -h 80% eme
```

Reload tmux:

```bash
tmux source-file ~/.tmux.conf
```

## Quick start

1. Press `<prefix> a` to open the dashboard.
2. Press `n` and pick a project folder.
3. `eme` creates `<folder>/main`, starts a tmux session, and opens the dashboard again.
4. Press `c`, type a worktree name, and press `Enter`.
5. Press `a` to launch your AI agent in that worktree.

## Commands

```text
eme                # dashboard
eme new            # fuzzy folder picker → new project + main worktree
eme switch <id>    # switch to session/window
eme kill <id>      # remove worktree + kill window
eme agent <id>     # start/stop/toggle agent
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

## Requirements

- tmux >= 3.0
- git >= 2.30
- A terminal that supports tmux popups
- An AI agent on your PATH (opencode, claude, codex, etc.)

## Why eme?

- **Mission control for parallel agents**: one dashboard shows every agent as `idle`, `working`, `waiting-for-input`, or `exited` — jump to the one that needs you.
- **Native to your tmux**: agents run in your real tmux sessions and windows (full compat, vim-modal, popup) — not a hidden tmux a TUI owns, not an Electron app.
- **git worktree-native**: each agent gets an isolated worktree and its own tmux window. `folder = project = tmux session`, no registration step.
- **Stays out of your way**: no diff/merge/approval GUI — review stays in your editor and git.

## Documentation

- [docs/usage.md](docs/usage.md) — workflow, config, keybindings
- [docs/adr/01-architecture.md](docs/adr/01-architecture.md) — architecture decisions

## License

MIT
