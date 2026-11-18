# Contributing to BurnBar

Bug fixes, provider integrations, accessibility improvements and documentation
fixes are welcome. Search [existing issues](https://github.com/nooblk-98/burnbar/issues)
before starting; for a larger change, open an issue to agree on scope first.

## Set up a development build

You need [Rust 1.80+](https://rustup.rs/). On Linux install WebKitGTK and
AppIndicator:

```bash
sudo apt install libwebkit2gtk-4.1-dev libayatana-appindicator3-dev librsvg2-dev
```

```bash
git clone https://github.com/nooblk-98/burnbar.git
cd burnbar/rust
cargo run -p burnbar-desktop -- --demo
```

`--demo` shows sample data for every provider and writes nothing; real usage
needs the matching local login. `--providers claude|codex|opencode|cursor|both|all`
limits one run without saving the choice, and `--hidden` starts in the tray.

The portable logic is in `rust/crates/burnbar-core`; the Tauri shell and UI are
in `rust/apps/desktop`. Do not add a platform-specific copy of a feature: use a
Tauri API or plugin so Windows, macOS and Linux share the code. Keep provider
access read-only and never commit credentials or personal usage responses.

## Check your change

```bash
cd rust
cargo fmt --all --check
cargo clippy --workspace --all-targets -- -D warnings
cargo test --workspace
./scripts/smoke.sh        # real binary, headless, demo data
```

Add tests for parsing, credential-store reads, milestones, quiet hours or
forecasts when changing them. For UI changes, check light and dark themes, the
125% and 150% text sizes, and a window resized to the minimum dimensions.

## Project skills

Optional agent skills are pinned under `.agents/skills` (`skills-lock.json`):
skill discovery, Tauri, Rust best practices, frontend design and codebase
cleanup. They are documentation for coding agents and do not affect the build.

```bash
npx skills find <query>
npx skills experimental_install
npx skills update
```

Review a skill before use; skills run with the agent's permissions.

## Add a provider

Provider fetchers live in `rust/crates/burnbar-core/src/`. Keep three things
separate:

1. Reading the provider's local credential material (read-only).
2. Fetching and decoding usage (no UI or OS dependencies).
3. Mapping the result to `Usage` / `Window` so history, milestones, alerts and
   forecasts work without provider-specific code.

Read stores through `dirs` or an environment override, never a hard-coded
platform path. Add a parser test, a mocked-HTTP test, and a credential-store test
where applicable. Document new network or local-data access in
[PRIVACY.md](PRIVACY.md).

## Releasing

See [docs/RELEASING.md](docs/RELEASING.md).
