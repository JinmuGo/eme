# eme

> Eeny, meeny, miny, moe 

AI agent session manager for git worktrees.

`eme` spins up a tmux session for every project folder, keeps each git worktree in its own window, and launches your AI agent with one key.

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

- **folder = project = tmux session**: no registration step.
- **git worktree-native**: each branch gets an isolated directory and tmux window.
- **AI agent-first**: launch, track, and switch between agent sessions from one dashboard.
- **vim-like modal TUI**: works inside a tmux popup or standalone.

## Documentation

- [docs/usage.md](docs/usage.md) — workflow, config, keybindings
- [docs/adr/01-architecture.md](docs/adr/01-architecture.md) — architecture decisions

## License

MIT
