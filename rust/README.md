# BurnBar — Rust + Tauri app

The application is one Rust workspace: portable usage logic in
`crates/burnbar-core`, a Tauri 2 tray shell in `apps/desktop`, and one
HTML/CSS/JS interface for Windows, macOS and Linux.

<img src="../docs/images/rust-tauri-demo.png" alt="BurnBar tray window with provider usage cards" width="430">

```
rust/
├── Cargo.toml                  workspace
├── crates/burnbar-core/        portable logic — no window, tray or platform code
│   ├── src/usage.rs            Provider, Window, Usage, demo data
│   ├── src/claude.rs           Claude Code credentials + usage API
│   ├── src/codex.rs            codex app-server JSON-RPC client
│   ├── src/opencode.rs         OpenCode Go credential store + usage API
│   ├── src/cursor.rs           Cursor state store + dashboard usage API
│   ├── src/config.rs           settings + alert state
│   ├── src/history.rs          JSONL readings and chart data
│   ├── src/alerts.rs           milestones, thresholds, quiet hours
│   ├── src/insights.rs         health, freshness, pace forecasts
│   ├── src/updates.rs          public stable-release checks
│   ├── src/service.rs          refresh loop, persistence, UI snapshot
│   └── tests/e2e.rs            mock HTTP + full refresh cycle
└── apps/desktop/               the only platform-aware crate
    ├── src/lib.rs              Tauri commands, tray, notifications, autostart
    ├── ui/                     one interface for every OS
    └── tauri.conf.json         window, bundle and security policy
```

## Cross-platform by construction

No feature has a per-platform copy. The only platform `cfg`s are:

- `#[cfg(windows)]` around `CREATE_NO_WINDOW` when spawning the Codex CLI.
- `#[cfg(target_os = "macos")]` around the notification permission request.
- Tauri's standard `#[cfg_attr(mobile, tauri::mobile_entry_point)]`, inert on
  desktop.

Tray, window, notifications, autostart, global shortcut and single-instance
behaviour come from Tauri and its official plugins. OpenCode and Cursor stores are
located through `dirs` and opened with bundled SQLite, so the same code works on
every desktop OS. The crate also keeps Tauri's mobile crate types for a future
mobile target.

## Providers

| Provider | Usage source | Local material (read-only) |
| --- | --- | --- |
| Claude | Anthropic OAuth usage API | `~/.claude/.credentials.json` |
| Codex | `codex app-server` account RPCs | Codex CLI |
| OpenCode Go | `opencode.ai/zen/go/v1/usage` | `opencode.db` |
| Cursor | Cursor dashboard usage service | `state.vscdb` |

`both` means Claude + Codex (the original behaviour); **All providers** (`all`)
shows all four. `OPENCODE_DATA_DIR`, `OPENCODE_DB`, `OPENCODE_GO_API_KEY`,
`CURSOR_STATE_DB` and `CURSOR_ACCESS_TOKEN` are optional overrides. Nothing is
written to another app's database.

## Build and run

Rust 1.80+ is required. Linux also needs WebKitGTK and AppIndicator:

```bash
sudo apt install libwebkit2gtk-4.1-dev libayatana-appindicator3-dev librsvg2-dev
```

```bash
cd rust
cargo run -p burnbar-desktop -- --demo     # sample data, writes nothing
cargo run -p burnbar-desktop               # real provider usage
cargo test --workspace                     # 62 tests, no account needed
cargo clippy --workspace --all-targets -- -D warnings
./scripts/smoke.sh                         # real binary, headless, demo data
```

Release build and installers:

```bash
cargo build --release -p burnbar-desktop
cargo install tauri-cli --version "^2"
cd apps/desktop && cargo tauri build
```

Flags: `--demo`, `--hidden` (tray-only start), and
`--providers claude|codex|opencode|cursor|both|all` (one run, not saved).

## Data and privacy

Settings and history use the standard user config folder
(`%APPDATA%\BurnBar`, `~/Library/Application Support/BurnBar`,
`$XDG_CONFIG_HOME/BurnBar`) or `BURNBAR_HOME`. One history file is kept per
provider. Credentials stay in each provider's own store; see
[PRIVACY.md](../PRIVACY.md). `BURNBAR_HOME` also makes test profiles easy.

## Release process

`cargo test` and the headless smoke test run in CI for Linux; release builds run
on Windows, macOS and Linux. The Windows executable is published by
`.github/workflows/build.yml` and installed with npx. See
[docs/RELEASING.md](../docs/RELEASING.md) and
[CONTRIBUTING.md](../CONTRIBUTING.md).
