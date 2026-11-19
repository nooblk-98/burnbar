# Privacy and data access

BurnBar reads existing provider logins and shows usage locally. It does not proxy
provider traffic, run analytics, or upload usage. Provider services and CLIs have
their own policies.

## npx installation (Windows)

The npx command downloads the installer package from npm (or GitHub) and runs the
bundled PowerShell helper. No install/postinstall hook runs BurnBar automatically.

The [Windows helper](npm/windows/install.ps1) reads public release metadata from
`https://api.github.com/repos/nooblk-98/burnbar/releases` and downloads the
executable and its SHA-256 checksum over HTTPS from GitHub. It sends a
`BurnBar-Installer` User-Agent. It needs no provider login or GitHub token; if
`GITHUB_TOKEN` or `GH_TOKEN` is set, that token is used only for the metadata
request and never for asset downloads. The token is not stored.

It installs under `%LOCALAPPDATA%\Programs\BurnBar`, can create a Start menu
shortcut, and can stop a running BurnBar to replace it. Settings and history in
`%APPDATA%\BurnBar` are preserved. `--no-start` installs without launching.

## Provider access

BurnBar uses the existing provider logins and never asks for a password.

### Claude

Reads the OAuth token Claude Code stores in `~/.claude/.credentials.json`
(`CLAUDE_CONFIG_DIR` if set) and requests
`https://api.anthropic.com/api/oauth/usage` over HTTPS. When the access token
expires, BurnBar sends the stored refresh token to Anthropic over HTTPS and
saves the rotated tokens to that credential file. The usage response supplies
quota percentages, reset times and the product breakdown. Claude Code handles
the initial sign-in.

### Codex

Starts a short-lived local `codex app-server` process, initializes it, calls
`account/read` with token refresh disabled, then `account/rateLimits/read`. The
process is stopped afterwards. BurnBar does not start model turns or read Codex
credential files. `CODEX_HOME` and `BURNBAR_CODEX_PATH` select the CLI's
configuration/executable; the CLI controls its own service traffic.

### OpenCode Go

Reads only the `opencode-go` credential from OpenCode's local SQLite store
(`opencode.db`, opened read-only) and requests
`https://opencode.ai/zen/go/v1/usage` with a `burnbar/<version>` User-Agent. The
response supplies rolling, weekly and monthly allowance percentages. Data folders
are found through the standard platform data directory; `OPENCODE_DATA_DIR`,
`OPENCODE_DB` and `OPENCODE_GO_API_KEY` are optional overrides.

### Cursor

Reads only `cursorAuth/accessToken` from Cursor's local `state.vscdb` (opened
read-only). On macOS, encrypted tokens are unlocked with Cursor's existing
`Cursor Safe Storage` login-keychain item; BurnBar never stores the key or token.
The token is posted to Cursor's dashboard `GetCurrentPeriodUsage` service. The
response supplies the monthly plan percentage, cycle reset and auto/API splits.
`CURSOR_STATE_DB` and `CURSOR_ACCESS_TOKEN` are optional overrides.

## Local app data

Settings and history live in the standard user config folder — `%APPDATA%\BurnBar`
on Windows, `~/Library/Application Support/BurnBar` on macOS and
`$XDG_CONFIG_HOME/BurnBar` on Linux. `BURNBAR_HOME` selects another folder.

| File | Contents |
| --- | --- |
| `config.json` | Provider selection, refresh interval, popup delay, thresholds, snooze, quiet hours, theme/text size, autostart, hotkey and update preferences, plus milestone/alert state used to avoid duplicate notices |
| `history.jsonl` | Claude session and weekly percentages with timestamps |
| `history-codex.jsonl` | Codex percentages with timestamps |
| `history-opencode.jsonl` | OpenCode Go allowance percentages with timestamps |
| `history-cursor.jsonl` | Cursor plan percentages with timestamps |

History is compacted to eight days. The files are ordinary local files with your
account's access controls; BurnBar does not add encryption and never stores
provider tokens there.

## Notifications and startup

Milestone and alert notices are sent through the operating system, which applies
its own Focus/Do Not Disturb rules. BurnBar does not inspect foreground windows,
screen contents or other applications. Start at login is handled by the Tauri
autostart plugin for the current user and can be disabled in Settings.

## Update checks

When enabled, the app reads public latest-stable metadata from
`https://api.github.com/repos/nooblk-98/burnbar/releases/latest`. Requests carry a
`BurnBar update check` User-Agent and no credentials, usage history or account
identifiers. The app never downloads or installs an update automatically.

## Data removal and reports

Turn off Start at login, exit BurnBar and delete its executable and the BurnBar
config folder. This does not sign out provider CLIs or remove their data.

Before sharing a screenshot or log, remove account names and usage numbers. Demo
mode (`--demo`) uses synthetic readings and writes nothing. Never attach
credential files, tokens or private session logs to an issue.
