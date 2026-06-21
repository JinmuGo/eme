# eme

> Eeny, meeny, miny, moe.

**Mission control for your AI coding agents.** Which agent needs you? eme counts them out.

`eme` runs each AI coding agent in its own git worktree as a real tmux session, and shows you — across every worktree — which one is `waiting-for-input`, which is still `working`, and which has `exited`. All without leaving the terminal.

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

`eme` is native to your tmux: it operates on whatever tmux server you are
currently attached to, so picking a worktree moves your real client and the
popup closes. If you instead want one dedicated eme server shared across every
launch context (including plain shells outside tmux), pin it with
`[tmux] socket = "<name>"` in `~/.config/eme/config.toml` or
`EME_TMUX_SOCKET=<name>`. Run `eme doctor` to see which server is in use.

### Ambient status segment (optional)

Surface the signal in your tmux status bar so you see a crashed agent without
opening eme. Append the segment to your existing `status-right` — eme never edits
your config — and let tmux poll it:

```tmux
set -g status-interval 2
set -ga status-right '#(eme status --tmux)'
```

It is empty (a dark cockpit) when nothing needs you, and shows a glyph-led count
when agents crash — e.g. `✗2`. It reads on a monochrome or colorblind bar; color
is enhancement only. The amber `●` beacon for *waiting* agents is a fast-follow.

## Quick start

1. Press `<prefix> a` to open the dashboard.
2. Press `n` and pick a project folder.
3. `eme` creates `<folder>/main`, starts a tmux session, and opens the dashboard again.
4. Press `c`, type a worktree name, and press `Enter`.
5. Press `a` to launch your AI agent in that worktree (or `A` to pick which one).

## Commands

```text
eme                # dashboard
eme new            # fuzzy folder picker → new project + main worktree
eme switch <id>    # switch to session/window
eme kill <id>      # remove worktree + kill window
eme clean <id>     # reset a crashed/exited worktree's pane to idle
eme agent <id>     # start/stop/toggle agent
eme agent <id> --pick  # choose the worktree's agent from the catalog
eme status --tmux  # ambient status-bar segment (✗N when agents crash)
eme doctor         # verify environment
eme --version      # print version
```

## Configuration

`~/.config/eme/config.toml`:

```toml
[agent]
command = "opencode"   # default highlighted in the agent picker

# Optional: add or override catalog entries.
[[agents]]
name = "claude-resume"
command = "claude --resume"
```

You can override the agent per folder or per worktree from the dashboard.

When you run `eme new`, eme shows an agent picker (claude, codex, gemini, opencode, plus any `[[agents]]` you add) listing what's installed on your PATH; your choice launches in `main` and becomes the project default. Press `a` on a worktree to toggle its agent, or `A` to pick a different one.

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

`eme` is licensed under the MIT License — see [LICENSE](LICENSE).

Third-party open-source dependencies and their licenses are disclosed in
[THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md).
