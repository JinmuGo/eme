# eme — project guide for coding agents

`eme` is mission control for AI coding agents: it runs each agent in its own git
worktree as a real tmux session and surfaces which one is `waiting-for-input`,
`working`, or `idle`. Go (CLI/TUI) + a Vite marketing site under `web/`.

This repository is a submission to the **2026 오픈소스 개발자대회 (OSS Developer
Contest)**. The rules (`open-source.pdf`, kept local-only) impose ongoing
obligations — keep the repo compliant as you work.

## Open-source contest compliance

- **License (제8조①②③):** first-party code is **MIT** (`LICENSE`). Keep it MIT.
  Never add code or a dependency under a non-commercial / academic-only license,
  and never add a **copyleft** dependency (GPL/LGPL/AGPL/MPL/CDDL/EPL) — it would
  force relicensing of the MIT binary.
- **Third-party disclosure (제8조⑤⑥):** every used library, framework, font,
  icon, and adapted snippet must be listed in **`THIRD_PARTY_LICENSES.md`** with
  its source + license. When you add a Go module, an npm package, a web font, an
  icon set, or copy an approach from another project, **update that file in the
  same change.**
- **AI model (제9조 / 별표2):** eme **embeds and applies no AI model.** It
  orchestrates external agent CLIs (`claude`/`codex`/`gemini`/`opencode`) as
  local tmux subprocesses and makes **zero AI API calls** (no `net/http` in the
  Go tool). This is the 제9조②1다 / 별표2 "AI-integration-ecosystem" **exception**,
  not a restricted commercial-API wrapper. Do **not** bundle model weights or add
  a direct commercial-AI-API dependency to the tool — that would trigger the
  restriction. No model-info form (제9조④) is required because no model is embedded.
- **Public source (제10조):** the repo must stay **PUBLIC** on GitHub. If it wins,
  it must remain public for **5 years** (제10조③) or the award is revoked.
- **No plagiarism (제13조):** single-author, no foreign copyright headers in
  source. Attribute any borrowed work rather than copying it silently.

## Local-only docs — never commit to the public repo

These are internal planning/strategy docs. They live on **local disk only** and
must **not** appear in the public repo or its git history. They are gitignored:

- `DESIGN.md`, `UX-STRATEGY.md`
- `docs/prd.md`, `docs/dx-design.md`, `docs/qa-checklist.md`, `docs/oss-contest-checklist.md`
- `docs/superpowers/**`, `.superpowers/**`
- `open-source.pdf` (the contest rules document)

If you create a new internal planning/strategy/spec doc, gitignore it too.
**Publishable** docs are: `README.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`,
`SECURITY.md`, `CHANGELOG.md`, `THIRD_PARTY_LICENSES.md`, `docs/usage.md`,
`docs/adr/`, and `docs/brand/`.

## Build / test

```bash
go build ./...      # build the CLI/TUI
go vet ./...
go test ./...       # all logic packages have tests
cd web && npm install && npm run build   # marketing site (build-time deps only)
```
