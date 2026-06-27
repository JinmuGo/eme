# eme Usage Guide

## Overview

`eme` maps a project folder to a tmux session and each git worktree to a tmux window. AI agents run inside those windows.

## Install

```bash
brew install alderwork/tap/eme                          # Homebrew (macOS / Linux)
curl -fsSL https://eme.jinmu.me/install.sh | sh       # install script
go install github.com/alderwork/eme/cmd/eme@latest      # from source
```

## Bind tmux

Add to `~/.tmux.conf`:

```tmux
bind-key a display-popup -E -w 80% -h 80% eme
```

Reload with `tmux source-file ~/.tmux.conf`. See [`examples/tmux.conf`](../examples/tmux.conf)
for the binding and [`examples/config.toml`](../examples/config.toml) for a fully-annotated config.

## Your first project

1. Press `<prefix> a` to open the dashboard.
2. Press `n` and pick a folder.
3. `eme` creates `<folder>/main`, a tmux session, and a window.
4. Press `c` and type a worktree name.
5. Press `a` to launch your AI agent (or `A` to pick which one).

## CLI commands

```text
eme                                  # dashboard
eme new [folder]                     # create project + main worktree
eme new --worktree <session> [name]  # create a worktree in an existing session
eme clone [owner/repo | url]         # clone a GitHub repo (gh) → project + main worktree
eme switch <session> [worktree]      # switch window
eme kill <session> [worktree] --force  # remove a worktree, or a whole session
eme clean <session> [worktree]       # revive a crashed/exited pane back to idle
eme agent <session> [worktree]       # toggle the agent
eme agent <session> [worktree] --pick  # choose the worktree's agent from the catalog
eme caffeinate <session> --mode manual|auto|off  # keep the Mac awake (macOS)
eme status --tmux                    # ambient tmux status-bar segment
eme hooks install | uninstall        # agent status hooks (Claude Code; opt-in)
eme forget <session>                 # stop managing a project (disk + tmux untouched)
eme doctor [folder]                  # verify environment / classify a folder
eme --version                        # print version
```

### Useful flags

- `--dry-run` — on `new` / `switch` / `kill` / `agent`: print the planned tmux/git actions without running them.
- `--verbose` — global: print every external command eme runs to stderr.
- `--config <path>` / `--state <path>` — global: override the config / state file locations.
- `eme new --agent <cmd>` — launch a specific agent non-interactively (`none` for a bare shell), skipping the picker.
- `eme new --convert <clone>` — losslessly restructure an existing normal clone into eme's nested-bare layout (keeps a backup; repos with submodules are refused — adopt them in place instead).
- `eme clone --dir <path>` — clone into `<path>/<repo>` instead of the configured clone dir.
- `eme clone --agent <cmd>` / `--no-switch` / `--dry-run` — same semantics as on `eme new`.
- `eme agent --set <cmd>` — set and launch a specific agent for the worktree without the picker.
- `eme kill --force-unpushed` — also delete a nested-bare project whose history is on no remote (implies `--force`).

## Configuration

`~/.config/eme/config.toml`:

```toml
[agent]
command = "opencode"

[[agents]]
name = "claude-resume"
command = "claude --resume"

[caffeinate]                # keep-awake (macOS); see "Keeping the Mac awake" below
flags = "-i"               # caffeinate flags; -i blocks idle system sleep (display still sleeps)
auto_grace_seconds = 60    # auto mode: stay awake this long after the last "working" sample

[tmux]
# socket = "eme"           # pin all tmux ops to one dedicated server (tmux -L eme); default: ambient

[status]
quiet_after = "2m"          # dim a hooked agent "working" longer than this; "0" disables

[picker]
# max_depth = 4            # how deep the new-project folder picker scans
# roots = ["~/src"]        # extra directories to scan for projects

[clone]
# dir = "~/Programming/new"  # where `eme clone` puts cloned repos; default: first existing
                             # of ~/Projects, ~/code, ~/src, ~/workspace, ~/dev, ~/Development (else ~/src)

[worktree]
# dir_template = "{repo}.worktrees"  # where worktrees for an adopted in-place clone are created
```

You can override the agent per folder or per worktree from the dashboard.

### Environment variables

- `EME_TMUX_SOCKET=<name>` — pin all tmux operations to one dedicated server (`tmux -L <name>`); same as `[tmux] socket`.
- `EME_THEME=light|dark` — force the color theme when eme can't detect the terminal background (e.g. inside some tmux popups).
- `EME_BEACON_COLOR=<color>` — override the amber beacon hue (any value lipgloss accepts, e.g. `#e69f00` or `214`).
- `EME_ASCII=1` — use ASCII status glyphs (`* o . x ·`) on terminals that can't render the Unicode dots.
- `EME_CLONE_DIR=<path>` — where `eme clone` puts cloned repos; same as `[clone] dir` (the `--dir` flag overrides both).

## Dashboard keys

The tree uses vim/nvim-style motions — sessions fold like a file tree.

| Key | Action |
|-----|--------|
| `↓` / `j`, `↑` / `k` | Move down / up (over session headers and worktrees) |
| `1`–`9` | Jump straight to that project (the number shown in its header row) |
| `[` / `]` | Jump to the previous / next project (steps past the first 9) |
| `→` / `l` | Expand a folded session, step into a session, or open a worktree |
| `←` / `h` | Fold a session (from a worktree, fold its parent and jump to the header) |
| `Enter` / `o` | On a worktree: switch to it · On a session header: toggle fold |
| `p` | Preview the selected worktree's live pane output in a side panel (read-only) |
| `n` | New project |
| `c` | Create worktree in the session under the cursor |
| `d` | Kill the worktree, or the whole project on a `main`/header row |
| `a` | Toggle agent in the selected worktree |
| `A` | Pick the selected worktree's agent from the catalog |
| `x` | Reset a crashed/exited worktree's pane back to idle |
| `w` | Cycle the session's keep-awake: `off → manual → auto → off` (macOS) |
| `?` | Toggle help |
| `q` / `Esc` | Quit |

After `d`, a confirm prompt appears: `y` removes the worktree (or whole project) and its
files · `f` forgets the project but keeps its files on disk · `D` force-deletes a project
whose history is on no remote.

## Creating a worktree (`c`)

Type a name and eme does the right thing:

- **New name** → creates a new branch and worktree.
- **An existing branch** → checks that branch out into the new worktree (works for a
  local branch, or a remote branch eme tracks for you).
- **A branch already checked out** in a worktree eme manages → switches you there
  instead of erroring (a branch can live in only one worktree).
- **A name that collides as a path** (e.g. `feat` when `feat/x` and `feat/y` exist) →
  eme refuses and lists those `feat/*` branches; type one to check it out.

## Precise agent status (hooks)

By default eme infers status from the pane's foreground process, which can tell `idle`
from "something running" but not `working` from `waiting-for-input`. To make eme match
the agent exactly, let the agent push its real state:

```bash
eme hooks install      # Claude Code: wire status hooks into ~/.claude/settings.json
eme hooks uninstall    # remove them
```

This is **opt-in** and **merge-safe** — it preserves every other setting and any hooks
you already have, backs up your settings to `settings.json.eme-bak`, and is idempotent.
Restart the agent (or start a new one) for the hooks to take effect. Under the hood the
hook stamps a tmux pane option (`@eme_state`) that eme reads in its normal status poll;
agents without it installed keep working with the foreground heuristic.

Currently only Claude Code exposes the lifecycle hooks eme needs. One known gap: Claude's
blocking choice menus (AskUserQuestion) don't fire the notification hook, so that
particular waiting state isn't surfaced yet.

## Keeping the Mac awake (caffeinate)

When you kick off a long agent run and walk away, macOS idle-sleep can suspend the work.
Designate a session to keep the Mac awake — per session, so only the projects you choose
hold the machine up:

```bash
eme caffeinate <session> --mode manual   # keep awake for the whole session
eme caffeinate <session> --mode auto     # keep awake only while an agent is working
eme caffeinate <session> --mode off      # stop
```

Or press **`w`** in the dashboard to cycle the session under the cursor
`off → manual → auto → off`. A session header shows **`(caf)`** for manual and
**`(caf~)`** for auto.

- **manual** holds a `caffeinate` assertion for as long as the session exists.
- **auto** holds it only while an agent in the session is `working` (it reuses the same
  status signal as the dashboard, so it works with or without the status hooks), and
  releases `auto_grace_seconds` after everything goes idle so brief gaps between agent
  turns don't drop sleep protection.

The assertion runs in a hidden `__eme_caffeinate` tmux window **inside the session**, so
it stops automatically the moment the session ends — there is no background daemon, and it
keeps working even when the dashboard is closed. Tune it under `[caffeinate]` in the config
(`flags`, default `-i` = block idle system sleep but let the display sleep;
`auto_grace_seconds`, default `60`). The whole feature is a no-op on non-macOS platforms.

## Worktree layout

For a **new, empty** folder, `eme` creates a nested bare repository:

```text
<folder>/
  .bare/       # bare git repo
  main/        # main worktree
  feature/     # additional worktree
```

If you point `eme new` at a folder that already contains a git repo, it is
adopted in place (the clone is the `main` worktree; new worktrees go to a sibling
`<repo>.worktrees/`).

### Cloning from GitHub (`eme clone`)

`eme clone` fetches a GitHub repo with the [gh CLI](https://cli.github.com) and
builds the **same nested-bare layout** as a new project, seeded from the remote:

```text
eme clone                 # fuzzy-pick from your GitHub repos (gh repo list)
eme clone alderwork/eme     # OWNER/REPO
eme clone https://github.com/alderwork/eme   # or a URL (https / ssh)
eme clone eme             # bare name → your own login (gh default)
```

The repo lands at `<clone-dir>/<repo>/` with `.bare/` + a `main/` worktree
checked out to the remote's default branch (`main`, `master`, …), then eme creates
the tmux session, onboards your agent, and switches in — exactly like `eme new`.

**Clone directory** is resolved in order: `--dir` flag → `EME_CLONE_DIR` →
`[clone] dir` in config → the first existing of `~/Projects`, `~/code`, `~/src`,
`~/workspace`, `~/dev`, `~/Development` → `~/src`. The clone dir is also scanned by
the `eme new` folder picker, so cloned repos are findable there later.

`eme clone` requires `gh` installed and authenticated (`gh auth login`); core eme
works without it. Run `eme doctor` to see gh status. Cloning into an existing
**registered** project just switches to it; a non-empty unregistered directory is
never overwritten.

### Plain (non-git) folders

If you pick a folder that already has **content but is not a git repo** — for
example a multi-repo parent directory you want to run a top-level agent in — `eme`
adopts it as a **plain** project: it runs the agent in the folder directly and
creates no `.bare`/`main/` scaffolding. A plain project is a single window at the
folder root. Because there is no git, worktree creation (`c`) is unavailable — run
`git init` in the folder (and re-add it) if you want worktree-per-agent.

Run `eme doctor <folder>` to see which action a folder would take.

## Troubleshooting

- **tmux server is not running**: start one with `tmux new-session -d`.
- **Agent not found**: install the agent or set `agent.command`.
- **Session name ambiguous**: use the full session id shown in `eme`.
