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

- Adopt an existing (non-bare) git clone in place: `eme new <existing-clone>` registers the clone as a project without restructuring it. New worktrees are created in a sibling `<repo>.worktrees/` container, and the adopted clone's directory is never deleted by `eme kill`.
- `eme forget <session>` removes a project from eme without touching disk or tmux — the disk-safe way to stop managing an adopted clone.
- `[worktree] dir_template` config (default `{repo}.worktrees`) controls where in-place worktrees are created; the template must resolve to a sibling of the repo.
- `eme doctor <folder>` classifies a folder's adopt-ability (greenfield, normal repo, submodule, bare, …); plain `eme doctor` additionally audits registered in-place projects for moved roots and prunable worktrees.

### Planned

- Full worktree-level agent status polling in the dashboard.
- Per-folder and per-worktree agent command overrides exposed through the UI.
- Prebuilt binaries and Homebrew formula.
