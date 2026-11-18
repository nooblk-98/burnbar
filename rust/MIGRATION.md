# Migration notes

This rewrite was planned with [rust-anything](https://github.com/erkamyaman/rust-anything)
1.4.0 (`npm install -g rust-anything`) and recorded in `.mcp.json` so coding
agents can use its MCP tools in this repository.

```bash
rust-anything analyze . --whole-app        # output: MIGRATION-ANALYSIS.txt
rust-anything analyze . --whole-app --json # machine-readable variant
```

The tool classified the repository as a CLI because of `npm/bin/burnbar.cjs`
and `cmd/burnbar`; the actual Go program is a native Windows/macOS GUI. That
changed the porting shape: instead of a clap front door (`app front-door`) the
new entry point is a Tauri application, and the units below were ported as a
workspace crate plus one shell crate.

## Units, in rust-anything's port order

| Wave | Go unit | Rust destination | Status | Evidence |
| ---: | --- | --- | --- | --- |
| 0 | `internal/updates/` | `burnbar-core::updates` | ported | 2 unit tests + mocked GitHub E2E |
| 0 | `internal/usage/` | `burnbar-core::{usage,claude,codex}` | ported | 10 tests + mocked HTTP E2E |
| 0 | `internal/layout/` | Tauri window placement | replaced | one window, centered/tray-first |
| 1 | `internal/alerts/` | `burnbar-core::alerts` | ported | 7 tests, threshold/quiet parity |
| 1 | `internal/history/` | `burnbar-core::history` | ported | 4 tests, same JSONL files |
| 2 | `internal/config/` | `burnbar-core::config` | ported | 5 tests, reads a real Go profile |
| 2 | `internal/insights/` | `burnbar-core::insights` | ported | 3 tests |
| 3 | `internal/render/` | `apps/desktop/ui/` | replaced | HTML/CSS cards and canvas charts |
| 4 | `internal/app/` | `burnbar-core::service` + Tauri shell | split | 6 service tests + headless smoke |
| 5 | `cmd/burnbar/` | `apps/desktop/src/main.rs` | replaced | `cargo run -p burnbar-desktop` |
| — | `internal/app/win32.go`, `run_darwin.go`, `macos_d.m` | Tauri tray/window/notification plugins | removed | no per-platform app code |

`tools/genres/` (icon and Windows resource generation) is not ported: Tauri
bundles icons from `apps/desktop/icons/` and adds version metadata through
`tauri.conf.json`, so there is no resource compiler in the build.

## rust-anything's dependency decisions

The raw analysis lists 12 decisions. Their outcomes:

| Decision | Dependency | Outcome |
| --- | --- | --- |
| D1 | shape | Keep the GUI shape; use a Tauri shell rather than a clap front door. |
| D2, D3 | `tc-hib/winres`, `winres/version` | Dropped. Tauri bundles icons/manifests. |
| D4–D8 | `golang.org/x/image/*` | Dropped. The UI draws bars and charts in the web view; no runtime font/image rendering in Rust. |
| D9, D10 | `golang.org/x/sys/windows`, `registry` | Dropped. Tauri plugins cover tray, startup and notifications. |
| D11, D12 | `golang.org/x/image`, `golang.org/x/sys` | Unused by the new code. |

New crates: `chrono`, `reqwest` (rustls), `serde`, `serde_json`, `dirs`, `glob`,
Tauri 2 and its `autostart`, `notification`, `opener`, `global-shortcut` and
`single-instance` plugins.

## Parity checks

Behaviour was compared against the Go source, not guessed:

- **Claude parsing** handles both the current `limits[]` shape and the legacy
  `five_hour` / `seven_day` objects, rejects out-of-range percentages, derives
  5h/7d window lengths, and keeps the Go label strings (`Session (5h)`,
  `Weekly (all models)`).
- **Codex parsing** prefers `rateLimitsByLimitId["codex"]`, falls back to
  `rateLimits`, keeps per-bucket extras sorted, renames a non-weekly secondary
  window to `Secondary`, and takes window lengths from Codex.
- **Milestones** use the first valid reading as a baseline, store high-water
  marks per provider/window, rearm on a new reset, honour custom thresholds and
  never move on errors or rate limits.
- **Alerts** keep one 80/95 level per reset cycle, reset when the percentage
  drops in a new cycle, and update state even when notifications are muted.
- **Config** reads and writes the same camelCase JSON. A fixture based on the Go
  schema is loaded in `config::tests::reads_a_go_profile_and_applies_the_popup_migration`.
- **History** keeps the same file names, 8-day retention, 64 KiB damaged-line
  guard, append-then-hourly-compaction strategy and sample JSON (`t`, `s`, `w`).
- **Forecasts** require three contiguous observations, 20 minutes, 1 percentage
  point of growth and the current quota cycle, and text matches the Go wording.
- **Updates** only accept a stable `x.y.z` tag whose link is the repository's own
  release URL; BurnBar never installs the update.

## Verification

```bash
cd rust
cargo fmt --all --check
cargo clippy --workspace --all-targets -- -D warnings
cargo test --workspace
./scripts/smoke.sh
```

The 62 tests include a local HTTP server (`tests/e2e.rs`) that exercises a 200
response, a 401, a 429 with `Retry-After`, a mocked GitHub release, mocked
OpenCode Go and Cursor responses, and a four-provider refresh where the
schema-backed providers are exercised through their parsers. The Linux CI job also runs the real
binary for 12 seconds under Xvfb + D-Bus; the build matrix compiles it on
Windows and macOS.

## Providers added after the Go port

Two providers do not exist in the Go app and were added directly in Rust:

| Provider | Unit | Source | Why it fits the model |
| --- | --- | --- | --- |
| OpenCode Go | `burnbar-core::opencode` | OpenCode's read-only SQLite credential store + `opencode.ai/zen/go/v1/usage` | Reports rolling (5h), weekly and monthly allowance percentages with reset times |
| Cursor | `burnbar-core::cursor` | Cursor's read-only `state.vscdb` + dashboard `GetCurrentPeriodUsage` | Reports the monthly plan percentage, cycle reset, and auto/API splits |

Neither provider needs a per-platform branch: `dirs` resolves the data/config
folders on Windows, macOS and Linux, and `rusqlite` with the bundled SQLite
library reads the local stores identically everywhere. Both sources are covered
by parse, credential-store and mocked-HTTP tests.

## What the recording cannot prove

There is no Go test suite to record from and the Go entry point is a native GUI,
so whole-app `app record` / `app replay` cases are not available on Linux. The
protection here is source-level parity plus the tests above. The following are
intentionally outside the recording:

- Windows fullscreen and shell-focus detection (replaced by OS notification
  policy).
- Pixel-identical rendering (the UI is intentionally new).
- Long-running tray behaviour under sleep/resume and DPI changes; manual smoke
  testing is recommended before replacing the Go app at the package level.
