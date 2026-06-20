# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-06-19

### Added

- `eme` dashboard opens in a tmux popup or standalone TUI.
- `eme new` creates a project with a nested bare repo and main worktree.
- `eme new --worktree <session>` creates additional worktrees.
- `eme switch` / `eme kill` / `eme agent` flat commands.
- `eme doctor` checks tmux, git, popup support, and agent availability.
- `eme --version` prints the current version.
- Built-in fuzzy folder picker and worktree name input.
- Structured Elm-style error messages with suggested fixes.
- Hybrid state synchronization with live tmux and git state.
- Atomic, locked state file writes.
- Runner interface for testable git/tmux shell-outs.
- Unit tests for session ids, state persistence, git commands, and name resolution.

## [Unreleased]

### Added

- Agent catalog: `eme new` shows an agent picker (built-in claude, codex, gemini, opencode, plus any `[[agents]]` you add) listing what's installed on your PATH; your choice launches in `main` and becomes the project default. Press `a` on a worktree to toggle its agent, or `A` to pick a different one (`eme agent --pick`). Override or extend the catalog with `[[agents]]` entries in config.
- Adopt an existing (non-bare) git clone in place: `eme new <existing-clone>` registers the clone as a project without restructuring it. New worktrees are created in a sibling `<repo>.worktrees/` container, and the adopted clone's directory is never deleted by `eme kill`.
- `eme forget <session>` removes a project from eme without touching disk or tmux — the disk-safe way to stop managing an adopted clone.
- `[worktree] dir_template` config (default `{repo}.worktrees`) controls where in-place worktrees are created; the template must resolve to a sibling of the repo.
- `eme doctor <folder>` classifies a folder's adopt-ability (greenfield, normal repo, submodule, bare, …); plain `eme doctor` additionally audits registered in-place projects for moved roots and prunable worktrees.
- `eme new --convert <clone>` restructures an existing normal clone into eme's nested-bare layout losslessly: it hard-links the gitdir, builds the new layout in a temporary directory, and atomically swaps it in while keeping a full backup (printed on success — delete it once verified). Repositories with submodules are refused in v1 (adopt them in place instead).
- `eme new --no-switch` creates a project or worktree without switching the tmux client to it (used by the dashboard).

### Changed

- The `eme` dashboard is now a persistent control center: create (`n`), create-worktree (`c`), agent (`a`), and kill (`d`) run in place and return to the refreshed dashboard instead of exiting. Only Enter/`o` switches away to a session. Killing now asks for confirmation.
- The `eme` dashboard is restyled (claude-squad-inspired): a full-screen rounded-border panel over a two-level session → worktree tree. Worktree rows lead with their agent status (`working`/`exited`/`idle`), the row under the cursor is a full-width highlight bar, and columns are aligned. The header carries the `eeny · meeny · miny · moe` motif with a right-aligned `N needs you` counter; the footer is pinned to the bottom. Navigation is per-worktree; `d` kills the selected worktree (or the whole project on a `main` row) after confirmation; `↵` opens it.

### Fixed

- `eme agent` now launches the configured agent as a bare command in the worktree's pane (whose cwd is already the worktree) instead of appending the worktree path as an argument. The trailing path only suited `opencode`; claude/codex/gemini now start correctly.
- Dashboard `d` (kill) now works: it confirms, then removes the session. Previously it launched `eme kill` without the required `--force` and silently did nothing.
- Cancelling the folder picker (Ctrl+C/Esc) in `eme new` no longer adopts the current directory and jumps to a stray tmux session — it simply returns.
- A standalone bare git repository is now classified correctly (bare, out of scope) instead of as a subdirectory, so `eme doctor`/`eme new` report it accurately.
- Running `eme` no longer dirties the shell. The dashboard renders on the alternate screen, so it leaves no scrollback behind, and switching with Enter now quits the TUI cleanly before exec'ing `eme switch` instead of replacing the process mid-render and leaving the terminal in raw/alt-screen state. The `eme new` folder picker and worktree-name prompt also use the alternate screen.

### Planned

- Full worktree-level agent status polling in the dashboard.
- Prebuilt binaries and Homebrew formula.
