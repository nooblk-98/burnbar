# Privacy and data access

This document describes BurnBar's own behavior in the current source code.
Provider CLIs and services have their own settings and policies.

## npx installation

The npx command downloads the installer package through npm (or GitHub when using
its GitHub package specifier); these services receive normal package-request
metadata. The dependency-free CLI invokes the PowerShell script bundled in that
package. No install/postinstall lifecycle hook runs BurnBar automatically.

The bundled [Windows helper](npm/windows/install.ps1) checks the repository's latest stable
release, or development prereleases when explicitly selected, using GitHub's API
and downloads its executable and checksum from GitHub
over HTTPS. It sends a `BurnBar-Installer` User-Agent; normal network metadata is
also visible to GitHub. It does not need a provider login or GitHub token.

It installs into `%LOCALAPPDATA%\Programs\BurnBar` by default, creates a Start
menu shortcut for the current user, and can close existing BurnBar processes
in the same Windows session to perform an update. It keeps the existing
`%APPDATA%\BurnBar` settings and histories. Use `--no-start` to install without
launching; in that mode it only closes an instance at the selected install path.

## Provider authentication and network requests

### Claude

BurnBar reads the OAuth access token saved by Claude Code in
`%USERPROFILE%\.claude\.credentials.json`, or the equivalent file under
`CLAUDE_CONFIG_DIR` when that environment variable is set. It uses the token to
request account usage from `https://api.anthropic.com/api/oauth/usage` over HTTPS.
The response supplies quota percentages, reset times, and available usage
breakdowns. BurnBar does not modify or refresh the credential file; Claude Code
handles sign-in and token renewal.

### Codex

BurnBar locates the installed Codex CLI and starts a short-lived local
`codex app-server` process. It initializes the connection, calls `account/read`
with token refresh disabled, then calls `account/rateLimits/read` to obtain
plan limits. The process is stopped when the request finishes. BurnBar does not
start model turns or conversations through that connection.

The Codex CLI handles its own authentication and service requests. BurnBar does
not read Codex credential files or copy Codex tokens. `CODEX_HOME` selects the
CLI's configuration location; `BURNBAR_CODEX_PATH` can select a native executable.
The CLI's configured services and network settings determine its network behavior.

## Local activity and Windows information

BurnBar watches file/directory change notifications under Claude Code's
`projects` folder and Codex's `sessions` folder to schedule usage refreshes.
The watcher does not read conversation contents, prompts, or source code.

For popup placement, BurnBar reads its notification icon's rectangle and the
monitor work area. It does not inspect taskbar controls or use UI Automation.
Fullscreen detection reads the foreground window's class, process filename, and
bounds, only to hide/suppress popups. Windows theme, high-contrast system colours and display scaling are also
read locally. BurnBar reads the shell notification state and, when supported,
Windows Focus-session status to suppress automatic notices. Quiet schedules and
snooze are evaluated locally; Windows quiet settings are never changed. Native tray tooltips are temporarily cleared while a card is open.

On its first normal launch with a new profile, BurnBar enables **Start with Windows**
by adding the executable to the current user's startup registry entry. Existing
preferences are preserved, and the option can be turned off in BurnBar's menu.
The choice is saved so later launches do not override an opt-out. Demo and
diagnostic modes do not automatically add a startup entry.

Choosing **Open GitHub repository** in the menu opens the project's public GitHub
page in your default browser. The About dialog itself does not make a network request.

## Stable update checks

When enabled (default), the app requests public latest-stable metadata from
`https://api.github.com/repos/nooblk-98/burnbar/releases/latest` at launch and at
most daily while running. Settings can disable automatic checks; Check now is
manual. Requests have a `BurnBar update check` User-Agent and no authorization
header, usage history, provider credentials or account identifiers. GitHub sees
normal network-request metadata. Release notes open GitHub in your browser.
Copy command writes only the displayed install/update command to your clipboard.
The app does not download or install an update automatically.

Forecasts are calculated locally from existing percentage/timestamp history.
The optional global Ctrl+Alt+B shortcut registers only that combination; it is
not a keyboard logger. Readable usage uses standard Windows accessibility controls.

## Data saved on your PC

BurnBar stores its files under `%APPDATA%\BurnBar\`:

| File | Contents |
| --- | --- |
| `config.json` | Provider selection, startup preference, popup/theme/text-size preferences and close delay, threshold lists, snooze expiry, local quiet-hour schedule, hotkey/update-check/forecast preferences, refresh interval, and alert state, including quota percentages, reset timestamps, and milestone high-water levels/window durations used to avoid duplicate popup notifications |
| `history.jsonl` | Claude session and weekly percentages with timestamps |
| `history-codex.jsonl` | Codex primary and secondary percentages with timestamps |

History is compacted to the most recent eight days when it is loaded. Files can
continue growing while the app stays running. These files are ordinary local
files, without application-level encryption, and use your Windows profile's
access controls. BurnBar does not save provider access tokens in these files.
Older `ClaudeUsageMeter` settings may be migrated into the BurnBar folder.

Usage responses are also held in memory to draw the meter and details panel.
BurnBar's code does not include telemetry, analytics, or cloud synchronization.
Its own direct HTTP usage request is to Anthropic, and optional update metadata requests go to GitHub; Codex service traffic is
handled by the installed CLI. Your system's network/proxy settings still apply.

## Diagnostics and shared reports

Usage summaries from `-once` and provider error messages can contain local paths.
They are not uploaded automatically. The old taskbar-layout diagnostic was removed
with the taskbar-widget implementation.

Before opening a public issue, review screenshots and output, remove account or
personal information, and never attach credential files, access tokens, or
private session logs. Demo mode (`-demo`) supplies synthetic usage readings and
does not fetch provider usage or write usage history, although it still loads
normal settings.

## Remove stored data

Turn off **Start with Windows** if enabled, then exit BurnBar and remove its
executable and `%APPDATA%\BurnBar\` folder to delete its settings and history.
This does not remove or sign out your provider CLIs. Keeping BurnBar running
while deleting its data can cause the files to be created again.

For implementation details, see [usage access](internal/usage),
[activity watching](internal/app/watch.go), [settings](internal/config/config.go),
and [history storage](internal/history/history.go).

## macOS preview

The [macOS port](docs/MACOS.md) stores settings/history under
`~/Library/Application Support/BurnBar` (or APPDATA when explicitly set for tests).
It reads the default Claude Code Keychain item via Security.framework without
background authorization prompts; only the menu's explicit authorization action
allows a prompt. Credentials stay in process memory, are never logged or persisted
by BurnBar, and are used only for the existing Anthropic usage endpoint. An explicit
custom Claude directory never falls back to another account's default Keychain.
Codex continues to use the local CLI account RPCs.

The preview does not install a login item, read macOS Focus settings, take desktop
screenshots, collect telemetry or send usage data to update servers. The macOS npx
helper downloads GitHub release assets/checksums, installs to the selected app
folder and optionally opens the app. It neither changes Keychain items nor removes
quarantine/system security settings. The app's search PATH additions apply only to
its process, never to shell configuration. Full beta limitations are in the guide.
