# Port map

Status of every Go unit and its Rust destination. "Ported" means behaviour was
compared against the Go source and is covered by tests in this workspace.

| Go source | Lines | Rust destination | State | Notes |
| --- | ---: | --- | --- | --- |
| `internal/usage/usage.go` | 274 | `burnbar-core/src/{usage,claude}.rs` | ported | credentials, HTTP status mapping, legacy + current JSON, breakdown |
| `internal/usage/codex.go` | 296 | `burnbar-core/src/codex.rs` | ported | executable discovery, app-server JSON-RPC, multi-bucket parsing |
| `internal/usage/retry.go` | 37 | `burnbar-core/src/usage.rs` | ported | `preserve_last_good` |
| `internal/usage/provider.go` | 50 | `burnbar-core/src/usage.rs` | ported | modes, provider names, demo data |
| `internal/usage/codex_windows.go` | — | `burnbar-core/src/codex.rs` | ported | `CREATE_NO_WINDOW` under `cfg(windows)` |
| `internal/usage/codex_other.go` | — | `burnbar-core/src/codex.rs` | replaced | one cross-platform spawn path |
| `internal/usage/credentials_darwin*.go` | — | `burnbar-core/src/claude.rs` | replaced | `~/.claude` works on every OS |
| `internal/config/config.go` | 135 | `burnbar-core/src/config.rs` | ported | same JSON, migration and normalisation |
| `internal/history/history.go` | 191 | `burnbar-core/src/history.rs` | ported | same files, retention, compaction |
| `internal/alerts/milestones.go` | 84 | `burnbar-core/src/alerts.rs` | ported | baseline, high-water, reset re-arm |
| `internal/alerts/preferences.go` | 80 | `burnbar-core/src/alerts.rs` | ported | thresholds, clocks, quiet policy |
| `internal/insights/insights.go` | 141 | `burnbar-core/src/insights.rs` | ported | health, freshness, forecast |
| `internal/updates/updates.go` | 103 | `burnbar-core/src/updates.rs` | ported | stable semver, URL validation |
| `internal/app/app.go` (logic) | 1120 | `burnbar-core/src/service.rs` | ported | refresh, alerts, milestones, snapshot |
| `internal/app/quiet.go` | 106 | `burnbar-core/src/alerts.rs` + shell | replaced | OS quiet time instead of WinRT |
| `internal/app/startup.go` | 91 | `apps/desktop/src/lib.rs` | replaced | autostart plugin |
| `internal/app/updates.go` | 93 | `apps/desktop/src/lib.rs` | ported | update command, notification |
| `internal/app/tray.go`, `controls.go`, `settings.go`, `insights.go` | 1022 | `apps/desktop/ui/` | replaced | web UI, same fields |
| `internal/app/win32.go` | 279 | Tauri | removed | no Win32 in the new app |
| `internal/app/run_darwin.go`, `macos_darwin.m` | 474 | Tauri | removed | no Cocoa in the new app |
| `internal/render/*` | 829 | `apps/desktop/ui/` | replaced | DOM bars, chips, canvas charts |
| `internal/layout/*` | 51 | Tauri window | replaced | one centered/docked window |
| `tools/genres/` | 117 | `apps/desktop/icons/` + `tauri.conf.json` | replaced | Tauri bundler |
| `cmd/burnbar/main.go` | 119 | `apps/desktop/src/main.rs` | replaced | `--demo`, `--hidden` |
| `npm/bin/burnbar.cjs` | 95 | unchanged | pending | npm installer cutover after platform sign-off |
| *(new)* OpenCode Go | — | `burnbar-core/src/opencode.rs` | new | read-only credential store + `/zen/go/v1/usage` |
| *(new)* Cursor | — | `burnbar-core/src/cursor.rs` | new | read-only `state.vscdb` + dashboard usage |

## Next steps after this PR

1. Manual tray/notification/autostart pass on Windows 10, Windows 11, macOS
   13+ (Intel and Apple silicon) and a GNOME/KDE Linux desktop.
2. Decide whether the Rust binary replaces the Go binary in the npm installer
   (Win) and the macOS installer, or ships as an additional optional channel.
3. If desired, use rust-anything `bridge`/`record` on a future CLI-only helper
   (for example a `burnbar --once` port) to add recorded-behaviour cases for
   non-GUI entry points.
