# Contributing to BurnBar

Contributors are welcome. Bug fixes, clearer documentation, accessibility
improvements, and additional AI tool integrations are all useful contributions.

## Start here

Search [existing issues](https://github.com/nooblk-98/burnbar/issues) before
starting work. For a larger change, open an issue to agree on the behavior and
scope before implementing it. The [roadmap](ROADMAP.md) describes current priorities.

Use the [bug report form](https://github.com/nooblk-98/burnbar/issues/new?template=01-bug-report.yml)
for problems and the [AI tool request form](https://github.com/nooblk-98/burnbar/issues/new?template=02-ai-tool-request.yml)
for new providers. General suggestions can use a blank issue.

## Set up a development build

Use Windows 10 or 11, Git, and Go 1.26 or newer. Fork the repository, clone your
fork, and create a branch for your change:

```powershell
git clone https://github.com/YOUR-USERNAME/burnbar.git
cd burnbar
git switch -c fix/describe-your-change
go mod download
go build -o bin/burnbar-dev.exe ./cmd/burnbar
.\bin\burnbar-dev.exe -demo -providers both
```

Demo mode supplies sample readings without fetching provider usage or writing
usage history or advancing/saving alert baselines. Settings are still loaded from
your normal BurnBar profile; explicit settings edits can be saved.
Only one BurnBar instance can run at a time, so exit the existing meter first.
Use a real provider login only when testing behavior that requires one.

The normal `build.ps1` script also runs checks and creates a console-free build.
It stops a running BurnBar process before replacing the executable. Add
`-Resources` only when you need to regenerate the embedded icon, manifest, or
version resource.

## Check your change

Format the Go files you changed with `gofmt`, then run:

```powershell
go vet ./...
go build ./...
```

Test sources are kept locally and ignored by Git. Run any local tests you maintain
before submitting a change, and include the results in your pull request.

For UI changes, check both light and dark themes, display scaling, and the
notification-area placement. Check automatic and manual popup expiry at different
close delays, tooltip suppression/restoration, Search/Start, fullscreen apps and
notification icons moved into or out of the overflow area.
Use local regression checks for behavior changes where they can catch the original bug.
Documentation-only changes need a review of links, examples, and formatting.

Use demo mode for public screenshots. Never commit credentials, personal usage
responses, session logs, or account identifiers. See [PRIVACY.md](PRIVACY.md).

## Project skills

This repository keeps optional agent skills under `.agents/skills/`, pinned in
`skills-lock.json`: `find-skills` (skill discovery), `tauri-v2`,
`rust-best-practices` and `frontend-design`. They are documentation for coding
agents and do not affect the build.

```bash
npx skills find <query>              # search skills.sh
npx skills experimental_install      # restore the pinned project skills
npx skills update                    # refresh them deliberately
```

Review an installed skill before use: skills run with the agent's permissions.
Do not add a skill that needs secrets or network access without documenting it
here first.

## Cross-platform Rust app

The Tauri implementation in `rust/` is the cross-platform rewrite. The portable
domain logic belongs in `rust/crates/burnbar-core`; windows, trays, notifications
and commands belong in `rust/apps/desktop`. Do not add a platform-specific copy of
a feature: use a Tauri API or plugin so Windows, macOS and Linux share the code.

```bash
cd rust
cargo fmt --all --check
cargo clippy --workspace --all-targets -- -D warnings
cargo test --workspace
cargo run -p burnbar-desktop -- --demo
```

On Linux install `libwebkit2gtk-4.1-dev`, `libayatana-appindicator3-dev` and
`librsvg2-dev` first. For headless smoke tests use
`xvfb-run -a dbus-run-session -- ./target/debug/burnbar --demo`. Keep the
`config.json` and `history*.jsonl` formats compatible with the Go app, and add a
test when changing parsing, milestones, quiet hours or forecasts. Architecture
notes and the unit map are in [`rust/README.md`](rust/README.md),
[`rust/DECISIONS.md`](rust/DECISIONS.md) and [`rust/MAP.md`](rust/MAP.md); the
port was planned with [rust-anything](https://github.com/erkamyaman/rust-anything)
(`.mcp.json` registers its MCP server).

## Add an AI tool integration

Start with a [provider request](https://github.com/nooblk-98/burnbar/issues/new?template=02-ai-tool-request.yml)
describing the tool, account type, quota windows, and any official API or CLI.
An integration needs a reliable, permitted way to read usage and clear handling
of missing login, unavailable quotas, timeouts, and rate limits.

Provider fetching and parsing live in `internal/usage`. Keep parsing separate
from network or process access so it can be tested using synthetic fixtures.
The shared `usage.Usage` and `usage.Window` types feed rendering and history.
Check provider selection, menus, refresh/backoff state, milestone windows, and separate
history storage when adding another provider; registration is currently explicit
across those areas. Use reported quota durations and reset times when available.

Reuse a provider's existing authenticated CLI or supported account mechanism
where practical. Document all new data access and network behavior in
[PRIVACY.md](PRIVACY.md). Do not submit real tokens or private account fixtures.

## Submit a pull request

Explain the problem, resulting behavior, and checks you ran. Link the related
issue and include demo screenshots for visual changes. Keep unrelated changes
in separate pull requests. Contributions are made under the repository's
[MIT license](LICENSE).
