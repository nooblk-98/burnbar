# BurnBar for Rust + Tauri

One Rust implementation of BurnBar that runs on **Windows, macOS and Linux**
without per-platform copies of the app. The original Go/Win32/Cocoa application
stays in the repository as the behavioural reference; this folder is the
cross-platform rewrite.

<img src="../docs/images/rust-tauri-demo.png" alt="BurnBar running in demo mode with provider usage cards" width="430">

```
rust/
├── Cargo.toml                  workspace
├── crates/burnbar-core/        portable logic — no windows, no tray, no UI
│   ├── src/usage.rs            Provider, Window, Usage, demo data
│   ├── src/claude.rs           Claude Code credentials + usage API
│   ├── src/codex.rs            codex app-server JSON-RPC client
│   ├── src/opencode.rs         OpenCode Go credential store + usage API
│   ├── src/cursor.rs           Cursor state store + dashboard usage API
│   ├── src/config.rs           settings + alert state (Go-compatible JSON)
│   ├── src/history.rs          JSONL readings and charts data
│   ├── src/alerts.rs           milestones, thresholds, quiet hours
│   ├── src/insights.rs         health, freshness, pace forecasts
│   ├── src/updates.rs          public stable-release checks
│   ├── src/service.rs          refresh loop, persistence, UI snapshot
│   └── tests/e2e.rs            mock HTTP + full refresh cycle
└── apps/desktop/               the only platform-aware crate
    ├── src/lib.rs              Tauri commands, tray, notifications, autostart
    ├── ui/                     one HTML/CSS/JS interface for every OS
    └── tauri.conf.json         window, bundle and security policy
```

## Why Tauri

The Go app talks to Win32 on Windows and Cocoa on macOS (`internal/app/win32.go`,
`internal/app/run_darwin.go`, `internal/app/macos_d.m`). Supporting a third
platform that way means a third native UI. Tauri keeps the native shell — tray,
window, notifications, autostart, global shortcut — behind one Rust API and
renders the interface with the system web view. The result is a single set of
commands (`get_state`, `refresh_now`, `save_settings`, …) and a single UI for
all three platforms.

There is **no platform branching in application logic**. The only platform
`cfg`s in the new code are:

- `#[cfg(windows)]` around `CREATE_NO_WINDOW` when spawning the Codex CLI, so a
  console window does not flash from a tray app.
- `#[cfg(target_os = "macos")]` around the notification permission request.
- Tauri's standard `#[cfg_attr(mobile, tauri::mobile_entry_point)]` marker,
  which is inert on desktop.

Prompt, credentials, history paths, tray, notifications, startup, shortcuts and
themes are expressed once. Every provider's local state is found through `dirs`
or an environment override, so OpenCode and Cursor need no per-platform copy;
their SQLite stores are read with one bundled SQLite build on every target.
The shell also keeps Tauri's mobile crate types and `mobile_entry_point`, so a
mobile target can reuse the same `lib.rs` later without restructuring.

## Build and run

Requirements: Rust 1.80+ (CI uses stable), and on Linux the WebKitGTK and
AppIndicator development packages:

```bash
sudo apt install libwebkit2gtk-4.1-dev libayatana-appindicator3-dev librsvg2-dev
```

```bash
cd rust
cargo run -p burnbar-desktop -- --demo     # sample data, no account needed
cargo run -p burnbar-desktop                # real provider usage
cargo test --workspace                      # 62 tests, no network or account
cargo clippy --workspace --all-targets -- -D warnings
./scripts/smoke.sh                          # real binary, headless, demo data
```

Release build and installers:

```bash
cargo build --release -p burnbar-desktop
# installers (.msi/.dmg/.AppImage/.deb) need the Tauri CLI:
cargo install tauri-cli --version "^2"
cd apps/desktop && cargo tauri build
```

`--hidden` starts in the tray (used for login startup), `--demo` uses fabricated
numbers and writes nothing, `BURNBAR_DEMO=1` does the same, and
`--providers claude|codex|opencode|cursor|both|all` limits this run without
saving the choice.

## Providers

| Provider | Usage source | Local material (read-only) |
| --- | --- | --- |
| Claude | `api.anthropic.com/api/oauth/usage` | `~/.claude/.credentials.json` (`CLAUDE_CONFIG_DIR`) |
| Codex | `codex app-server` account RPCs | Codex CLI on `PATH` (`BURNBAR_CODEX_PATH`) |
| OpenCode Go | `opencode.ai/zen/go/v1/usage` | `opencode.db` under the OpenCode data folder (`OPENCODE_DB`, `OPENCODE_DATA_DIR`, `OPENCODE_GO_API_KEY` overrides) |
| Cursor | Cursor dashboard `GetCurrentPeriodUsage` | `state.vscdb` under the Cursor config folder (`CURSOR_STATE_DB`, `CURSOR_ACCESS_TOKEN` overrides) |

OpenCode and Cursor stores are read with SQLite in read-only mode; BurnBar never
writes a token or edits another app's database. The OpenCode credential store is
found through `dirs::data_local_dir()` (including OpenCode's Windows `Data`
subfolder) and Cursor through `dirs::config_dir()`, so the same code covers
Windows, macOS and Linux. `both` keeps its original Claude + Codex meaning;
choose **All providers** (`all`) to show all four.

## What carries over

- **Same profile folder and files.** `%APPDATA%\BurnBar`, `~/Library/Application
  Support/BurnBar` or `$XDG_CONFIG_HOME/BurnBar`, with `config.json`,
  `history.jsonl`, `history-codex.jsonl` and one history file per new provider.
  Existing Go profiles load unchanged, including `milestoneState`, `alertState`,
  thresholds, snooze and quiet hours. Set `BURNBAR_HOME` to use another folder.
- **Same credentials.** BurnBar only reads the credential stores above. It never
  writes tokens or opens a login flow.
- **Same behaviour.** 10% milestone tracking from a baseline, 80%/95% and reset
  alerts, threshold presets, quiet hours and snooze, a week of history, 24h/7d
  charts, pace markers and conservative forecasts — for every provider. Errors
  are kept as data, so a failed fetch never relabels old readings as fresh.
- **Same refresh model.** Tray-first window, background refresh every N
  minutes, tray menu with milestones/settings/about/quit, close-to-tray, and
  optional start at login.

Deliberate differences from the native Go shells:

- The popup is a web view, so the Windows-specific fullscreen detection and the
  native notice styles are replaced by ordinary tray-window behaviour and system
  notifications. Focus/quiet time is left to the operating system.
- Startup is handled by the Tauri autostart plugin instead of the Windows
  registry or a macOS launch agent written by hand.
- The `snapshot` PNG preview flag is replaced by `--demo` plus an ordinary
  window screenshot (`docs/images` can be regenerated from any OS).

See [MIGRATION.md](MIGRATION.md) for the rust-anything plan, unit mapping and
parity evidence, [DECISIONS.md](DECISIONS.md) for the architecture decisions and
[MAP.md](MAP.md) for the unit-by-unit status.
