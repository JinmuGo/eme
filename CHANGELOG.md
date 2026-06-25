# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Planned

- Full worktree-level agent status polling in the dashboard.

## [0.1.0] - 2026-06-25

### Added

- Foundational CLI: the `eme` dashboard opens in a tmux popup or standalone TUI;
  `eme new` creates a project with a nested-bare repo and `main` worktree;
  `eme new --worktree <session>` adds worktrees; flat `eme switch` / `eme kill` /
  `eme agent` commands; `eme doctor` checks tmux, git, popup support, and agent
  availability; `eme --version`. Built-in fuzzy folder picker and worktree-name
  input, structured Elm-style error messages with suggested fixes, hybrid state
  synchronization with live tmux and git state, atomic locked state-file writes,
  and a runner interface for testable git/tmux shell-outs.
- Distribution: prebuilt binaries (macOS/Linux · amd64/arm64) published on each
  `v*` tag via GoReleaser, a Homebrew tap (`brew install jinmugo/tap/eme`), an
  install script (`curl -fsSL https://eme.jinmu.me/install.sh | sh`), and a
  landing page at https://eme.jinmu.me.
- Agent catalog: `eme new` shows an agent picker (built-in claude, codex, gemini,
  opencode, plus any `[[agents]]` you add) listing what's installed on your PATH; your
  choice launches in `main` and becomes the project default. Press `a` on a worktree to
  toggle its agent, or `A` to pick a different one (`eme agent --pick`). Override or
  extend the catalog with `[[agents]]` entries in config.
- Agent state sync via hooks. `eme hooks install` wires Claude Code's lifecycle
  hooks (`UserPromptSubmit`/`Notification`/`Stop`) to stamp the agent's real state
  into a tmux pane option (`@eme_state`), which eme reads in its existing pane
  snapshot — so the dashboard distinguishes `working` from `waiting-for-input` from
  `idle` precisely, instead of only guessing from the foreground process. The install
  is opt-in, merge-safe (it preserves every other key and any foreign hooks, e.g. a
  SessionEnd hook from another tool), idempotent, backs up your settings, and writes
  atomically. `eme hooks uninstall` removes only eme's hooks. Agents without the hooks
  installed are unaffected (they keep the foreground heuristic). A shell prompt always
  wins as `idle`, so a stale `@eme_state` left by a crashed agent never misleads.
- Keep the Mac awake for a session (macOS). Designate a session with
  `eme caffeinate <session> --mode manual|auto|off`, or press `w` in the dashboard
  to cycle `off → manual → auto → off`. `manual` holds a `caffeinate` assertion for
  the whole session; `auto` holds it only while an agent is working in the session
  (reusing the same status signal as the dashboard, so it works with or without the
  status hooks) and releases after a short grace once everything goes idle. The
  assertion runs in a hidden `__eme_caffeinate` tmux window inside the session, so it
  stops automatically when the session ends — no background daemon, and it works even
  when the dashboard is closed. A session header shows `(caf)` / `(caf~)` for
  manual / auto. Tunable via `[caffeinate]` in the config (`flags`, default `-i`,
  prevents idle system sleep while letting the display sleep; `auto_grace_seconds`,
  default `60`). A no-op on non-macOS platforms.
- Adopt a plain (non-git) folder in place. Picking a folder that already has
  content but is not a git repo — e.g. a multi-repo parent directory you want to
  drive a top-level agent in — now registers it as a `plain` project and runs the
  agent in the folder directly, instead of scaffolding a nested-bare layout
  (`.bare` + an empty `main/`) into it (which left the existing content orphaned).
  Only a truly empty folder still gets the nested-bare scaffold. A plain project
  is a single window at its root; reconcile keeps it on the strength of the
  directory + tmux window alone (no git), and worktree creation refuses with a
  clear message (it needs git). `eme doctor <folder>` reports which action a
  non-git folder would take (scaffold vs adopt-in-place).
- Adopt an existing (non-bare) git clone in place: `eme new <existing-clone>`
  registers the clone as a project without restructuring it. New worktrees are
  created in a sibling `<repo>.worktrees/` container, and the adopted clone's
  directory is never deleted by `eme kill`.
- `eme new --convert <clone>` restructures an existing normal clone into eme's
  nested-bare layout losslessly: it hard-links the gitdir, builds the new layout in a
  temporary directory, and atomically swaps it in while keeping a full backup (printed
  on success — delete it once verified). Repositories with submodules are refused in
  v1 (adopt them in place instead).
- `eme new --no-switch` creates a project or worktree without switching the tmux
  client to it (used by the dashboard).
- `eme forget <session>` removes a project from eme without touching disk or tmux —
  the disk-safe way to stop managing an adopted clone.
- `[worktree] dir_template` config (default `{repo}.worktrees`) controls where
  in-place worktrees are created; the template must resolve to a sibling of the repo.
- `eme doctor <folder>` classifies a folder's adopt-ability (greenfield, normal repo,
  submodule, bare, …); plain `eme doctor` additionally audits registered in-place
  projects for moved roots and prunable worktrees, and reports which tmux server eme
  is using (ambient or a pinned socket).
- Optional `[tmux] socket` config (or `EME_TMUX_SOCKET`) pins all tmux operations to
  one dedicated server via `tmux -L <socket>`, for users who want a single eme server
  shared across every launch context. Default is unset (ambient): eme stays native to
  whatever tmux server you are currently on, so switching to a worktree moves your real
  client and the popup closes.

### Changed

- The `eme` dashboard is now a persistent control center: create (`n`),
  create-worktree (`c`), agent (`a`), and kill (`d`) run in place and return to the
  refreshed dashboard instead of exiting. Only Enter/`o` switches away to a session.
  Killing now asks for confirmation.
- The `eme` dashboard is restyled (claude-squad-inspired): a full-screen
  rounded-border panel over a two-level session → worktree tree. Worktree rows lead
  with their agent status (`working`/`exited`/`idle`), the row under the cursor is a
  full-width highlight bar, and columns are aligned. The header carries the
  `eeny · meeny · miny · moe` motif with a right-aligned `N needs you` counter; the
  footer is pinned to the bottom. Navigation is per-worktree; `d` kills the selected
  worktree (or the whole project on a `main` row) after confirmation; `↵` opens it.
- Creating a worktree now does the right thing when the name matches an existing
  branch instead of refusing. If `<name>` is an existing branch, eme checks it out
  into the new worktree (a local branch, or a remote branch it tracks via git's DWIM)
  rather than failing on "branch already exists". If that branch is already checked
  out in a worktree eme manages, it switches you there instead of erroring. On a
  directory/file name conflict (e.g. `feat` when `feat/x` branches exist), the error
  lists the real `feat/*` branches you can type to check one out. A brand-new name
  still creates a new branch as before.
- Agents now run as a **child of the pane's shell** (a bare command) instead of
  replacing the shell via `exec`. Quitting an agent (Ctrl-C / exit) returns to a
  live shell prompt in the same pane instead of leaving a frozen "Pane is dead"
  screen. Agent liveness and the dashboard status now read the pane's foreground
  process (`pane_current_command`): a shell prompt is `idle`, anything else is
  `working`. As a result `exited`/`crashed` statuses now occur only for a pane the
  user manually kills/exits; an agent that quits reads `idle`.

### Fixed

- Worktree creation now reports git's actual failure instead of a bare
  `exit status 255`: `git worktree add`'s stderr (e.g. `cannot lock ref
  'refs/heads/feat': 'refs/heads/feat/design-polish' exists`) is surfaced in the
  error details. eme also pre-checks directory/file branch-ref conflicts — a name
  like `feat` when `feat/x` branches already exist (or `feat/x` when `feat` is a
  branch) — and refuses with an actionable message naming the conflicting branch,
  before touching git.
- Reconcile no longer prunes (and persists) sessions when the tmux server is
  unreachable. Opening the dashboard while the server was down used to treat
  every session as dead and wipe it from state. This was the root cause of
  sessions "disappearing" between launches.
- `eme agent` now launches the configured agent as a bare command in the worktree's
  pane (whose cwd is already the worktree) instead of appending the worktree path as
  an argument. The trailing path only suited `opencode`; claude/codex/gemini now start
  correctly.
- Dashboard `d` (kill) now works: it confirms, then removes the session. Previously
  it launched `eme kill` without the required `--force` and silently did nothing.
- Cancelling the folder picker (Ctrl+C/Esc) in `eme new` no longer adopts the current
  directory and jumps to a stray tmux session — it simply returns.
- A standalone bare git repository is now classified correctly (bare, out of scope)
  instead of as a subdirectory, so `eme doctor`/`eme new` report it accurately.
- Running `eme` no longer dirties the shell. The dashboard renders on the alternate
  screen, so it leaves no scrollback behind, and switching with Enter now quits the
  TUI cleanly before exec'ing `eme switch` instead of replacing the process mid-render
  and leaving the terminal in raw/alt-screen state. The `eme new` folder picker and
  worktree-name prompt also use the alternate screen.

[Unreleased]: https://github.com/JinmuGo/eme/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/JinmuGo/eme/releases/tag/v0.1.0
