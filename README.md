<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/brand/hero-dark.png">
    <img src="docs/brand/hero.png" alt="eme — mission control for your AI coding agents" width="720">
  </picture>
</p>

<p align="center">
  <a href="https://pkg.go.dev/github.com/JinmuGo/eme"><img src="https://pkg.go.dev/badge/github.com/JinmuGo/eme.svg" alt="Go Reference"></a>
  <a href="https://github.com/JinmuGo/eme/actions/workflows/ci.yml"><img src="https://github.com/JinmuGo/eme/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://goreportcard.com/report/github.com/JinmuGo/eme"><img src="https://goreportcard.com/badge/github.com/JinmuGo/eme" alt="Go Report Card"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License: MIT"></a>
</p>

**Mission control for your AI coding agents.** Which agent needs you? eme counts them out.

`eme` runs each AI coding agent in its own git worktree as a real tmux session, and shows you — across every worktree — which one is `waiting-for-input`, which is still `working`, and which is back at an `idle` prompt. All without leaving the terminal.

## Install

```bash
# Homebrew (macOS / Linux)
brew install jinmugo/tap/eme

# or the install script
curl -fsSL https://eme.jinmu.me/install.sh | sh

# or from source with Go
go install github.com/JinmuGo/eme/cmd/eme@latest
```

If you use `go install`, make sure `$GOPATH/bin` is on your `PATH`.

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
eme                    # dashboard
eme new                # fuzzy folder picker → new project + main worktree
eme clone [owner/repo] # clone a GitHub repo (gh) → new project + main worktree
eme switch <id>        # switch to session/window
eme kill <id> --force  # remove worktree + kill window (--force required)
eme clean <id>         # reset a crashed/exited worktree's pane to idle
eme agent <id>         # start/stop/toggle agent
eme agent <id> --pick  # choose the worktree's agent from the catalog
eme caffeinate <id> --mode manual|auto|off  # keep the Mac awake (macOS)
eme status --tmux      # ambient status-bar segment (✗N when agents crash)
eme hooks install      # let the agent push precise status to eme (Claude Code; opt-in)
eme forget <id>        # stop managing a project (leaves disk + tmux untouched)
eme doctor             # verify environment
eme --version          # print version
```

Run `eme <command> --help` for flags; see [docs/usage.md](docs/usage.md) for the full reference.

### Hooks upgrade note

`eme hooks install` also stamps the moment each state changed (so the dashboard can show
how long an agent has been waiting) and scopes `waiting` to real permission prompts and
questions. If you installed eme's hooks before this, re-run `eme hooks install` once to
upgrade — it rewrites only eme's own hooks and preserves everything else.

In the dashboard: `s` floats the agents that need you to the top, `p` opens a live side
preview of the selected agent. Configure the gone-quiet
threshold in `~/.config/eme/config.toml`:

```toml
[status]
quiet_after = "2m"   # dim a hooked agent that's been "working" this long; "0" disables
```

## Configuration

`~/.config/eme/config.toml`:

Every key is optional; the values below are the built-in defaults — set only what you want to change. Override the file location with `eme --config <path>`.

```toml
[agent]
command = "opencode"          # default agent highlighted in the picker

# [[agents]]                  # add/override catalog entries (a matching name overrides a builtin)
# name = "claude-resume"
# command = "claude --resume"

[picker]
max_depth = 3                 # how deep the new-project folder picker scans
# roots = ["~/src"]           # extra dirs to scan, on top of the auto-discovered ones

[worktree]
dir_template = "{repo}.worktrees"   # sibling dir holding an adopted repo's worktrees

[clone]
# dir = "~/Projects"          # where `eme clone` puts repos; default: first existing of
                              # ~/Projects, ~/code, ~/src, ~/workspace, ~/dev, ~/Development

[tmux]
# socket = "eme"              # pin all tmux ops to one server; default: your current server

[status]
quiet_after = "2m"            # dim a hooked agent stuck "working" this long; "0" disables

[caffeinate]                  # keep-awake (macOS)
flags = "-i"                  # caffeinate flags; -i blocks idle system sleep
auto_grace_seconds = 60       # auto mode: stay awake this long after the last "working" sample
```

**Environment overrides:** `EME_THEME=light|dark`, `EME_BEACON_COLOR=<color>`, `EME_ASCII=1` (ASCII status glyphs for non-Unicode terminals), `EME_TMUX_SOCKET=<name>`, `EME_CLONE_DIR=<path>`.

See [`examples/config.toml`](examples/config.toml) for a fully-annotated version. You can override the agent per folder or per worktree from the dashboard.

When you run `eme new`, eme shows an agent picker (claude, codex, gemini, opencode, plus any `[[agents]]` you add) listing what's installed on your PATH; your choice launches in `main` and becomes the project default. Press `a` on a worktree to toggle its agent, or `A` to pick a different one.

## Requirements

- tmux >= 3.0
- git >= 2.30
- A terminal that supports tmux popups
- An AI agent on your PATH (opencode, claude, codex, etc.)
- [gh](https://cli.github.com) (GitHub CLI), authenticated — only for `eme clone`

## Why eme?

- **Mission control for parallel agents**: one dashboard shows every agent as `idle`, `working`, or `waiting-for-input` — jump to the one that needs you.
- **Native to your tmux**: agents run in your real tmux sessions and windows (full compat — copy-mode, popups, your own keybindings) — not a hidden tmux a TUI owns, not an Electron app.
- **git worktree-native**: each agent gets an isolated worktree and its own tmux window. `folder = project = tmux session`, no registration step.
- **Stays out of your way**: no diff/merge/approval GUI — review stays in your editor and git.

## Documentation

- [docs/usage.md](docs/usage.md) — workflow, config, keybindings
- [docs/adr/01-architecture.md](docs/adr/01-architecture.md) — architecture decisions
- [examples/](examples/) — annotated `config.toml` and a `tmux.conf` popup binding

## License

`eme` is licensed under the MIT License — see [LICENSE](LICENSE).

Third-party open-source dependencies and their licenses are disclosed in
[THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md).
