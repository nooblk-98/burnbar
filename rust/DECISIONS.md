# Decisions

Architecture decision records for the Rust/Tauri rewrite. Each records the
question, the answer, and why. The numbering follows the decisions produced by
`rust-anything analyze . --whole-app` (see `MIGRATION-ANALYSIS.txt`).

## D1 — Port shape: Tauri application, not a CLI front door

**Question.** rust-anything sees an npm CLI and `cmd/burnbar` and recommends a
clap front door. Is that the right shape?

**Decision.** No. Keep the application shape and use a Tauri 2 shell: one Rust
backend, one web UI, native tray/window/notification per platform.

**Why.** The shipped product is a tray GUI. A clap front door would test the
wrong thing. Tauri gives the cross-platform shell that the Go app reimplements
three times (Win32, Cocoa, future Linux) without per-platform application code.

## D2 — Keep the Go app in the repository

**Question.** Delete the Go implementation during the rewrite?

**Decision.** No. Add `rust/` alongside it. Cut over packaging when the Rust app
has been exercised on all three platforms.

**Why.** rust-anything's whole-app workflow keeps the original as the reference
until `app lift`. The Go code remains the behavioural specification and the
current Windows/macOS release path. Removing it in the same PR would make
rollback expensive.

## D3 — Share profile files with the Go app

**Question.** New config format for Rust?

**Decision.** Read and write the same `config.json`, `history.jsonl` and
`history-codex.jsonl` in the same folders. Keep camelCase keys and Go's
tolerant validation.

**Why.** Users who try the Rust build keep their settings, history, thresholds,
snooze and milestone state. It also gives a direct parity check: a real Go
profile is a test fixture.

## D4 — Portable core, one platform-aware shell

**Question.** Where do platform checks live?

**Decision.** `burnbar-core` has no windowing or OS-specific code.
`apps/desktop` uses Tauri APIs; only two `cfg` blocks exist (Windows console
hiding for the Codex child process, macOS notification permission).

**Why.** "Support every platform without changes per platform" is easiest to
enforce when the domain crate cannot see a platform. The shell uses Tauri's
cross-platform abstractions rather than hand-written Win32/Cocoa.

## D5 — Credentials: read-only, no new login flow

**Decision.** Read Claude Code's stored OAuth token and ask the Codex CLI over
`app-server`. Never write tokens, refresh them, or ask for a password.

**Why.** Matches the Go app and the privacy promise. The Codex client rejects
server-initiated RPC requests, so a local account probe cannot trigger tools.

## D6 — Errors are data, old readings stay old

**Decision.** A failed fetch produces a `Usage` with `err`/`rateLimited` and the
previous good windows are preserved without changing their timestamp.

**Why.** Matches `PreserveLastGood`; the UI can say "Offline" while still showing
the last reliable numbers, and forecasts refuse stale data.

## D7 — Milestones start from a baseline

**Decision.** The first valid reading of a window only records a high-water
mark. A notification fires when a configured threshold is crossed above that
mark in the same reset window; a new reset re-arms the tracker.

**Why.** Restarts, corrections and polling must not replay historical
milestones. State is persisted in the same `milestoneState` object.

## D8 — Quiet hours use local wall-clock time

**Decision.** Quiet hours are evaluated with `chrono::Local`; snooze is an
absolute timestamp. Equal start/end means a full quiet day when enabled.

**Why.** Users configure "22:00 to 08:00" in their own time zone. Using UTC
would silently shift quiet hours with DST and travel.

## D9 — Notifications are OS notifications; no custom fullscreen hook

**Decision.** Use Tauri's notification plugin and optionally show the tray
window for milestones. Do not reimplement Windows Focus Assist or fullscreen
detection.

**Why.** Every platform already filters notifications for Do Not Disturb and
fullscreen. Re-deriving that from shell state is brittle and was a source of
Windows-specific bugs in the Go app. The `respectWindowsQuiet` preference is
retained for profile compatibility and documents the OS-level behaviour.

## D10 — Security boundaries

**Decision.** The web view may call a fixed command list. It cannot read files
or the network directly. External links are restricted to the BurnBar repository
and npm page. CSP is set to `self` plus Tauri IPC; no remote content is loaded.

**Why.** The UI only needs serialized state. Keeping filesystem, credentials and
HTTP in Rust means a compromised or malformed page cannot read `~/.claude`.

## D11 — Autostart, hotkey and single instance through plugins

**Decision.** Use `tauri-plugin-autostart` (with `--hidden`), a global shortcut
(Ctrl+Alt+B) and `tauri-plugin-single-instance`.

**Why.** These replace Windows registry writes, a macOS launch agent and the
Win32 message-only window with one implementation that builds on all targets.
A second launch focuses the existing tray window instead of racing on history
files.

## D13 — OpenCode and Cursor read local stores through `dirs`

**Question.** OpenCode and Cursor keep credentials in app-specific SQLite
databases. Do they need platform-specific readers?

**Decision.** No. Locate the stores with `dirs::data_local_dir()` and
`dirs::config_dir()`, support the OpenCode Windows `Data` layout as an
additional candidate, and read them with one bundled SQLite build
(`rusqlite`/`libsqlite3-sys`) in read-only mode. Allow explicit environment
overrides for unusual setups.

**Why.** Cursor's folder is the standard config directory on all three
platforms; OpenCode's data folder follows `XDG`/macOS/Windows conventions.
Keeping candidate paths in one list avoids a second implementation and keeps the
"same code on every platform" rule. Reading only `cursorAuth/accessToken` and
`opencode-go` means BurnBar never enumerates or writes other app data.

## D12 — Update checks stay metadata-only

**Decision.** Read the public GitHub `releases/latest` JSON, validate the tag and
URL, and link to the release. Never download or execute an installer.

**Why.** Matches the Go app's deliberate no-auto-update stance and keeps the
app's network surface to the two usage APIs plus GitHub metadata.
