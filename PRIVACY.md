# Privacy and data access

This document describes BurnBar's own behavior in the current source code.
Provider CLIs and services have their own settings and policies.

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

For placement, BurnBar reads taskbar control classes and rectangles using Windows
UI Automation. Fullscreen detection reads the foreground window's class, process
filename, and bounds. These observations are used locally to position or hide
the meter. BurnBar also reads Windows theme, scaling, and animation settings.

On its first normal launch with a new profile, BurnBar enables **Start with Windows**
by adding the executable to the current user's startup registry entry. Existing
preferences are preserved, and the option can be turned off in BurnBar's menu.
The choice is saved so later launches do not override an opt-out. Demo and
diagnostic modes do not automatically add a startup entry.

Choosing **Open GitHub repository** in the menu opens the project's public GitHub
page in your default browser. The About dialog itself does not make a network request.

## Data saved on your PC

BurnBar stores its files under `%APPDATA%\BurnBar\`:

| File | Contents |
| --- | --- |
| `config.json` | Provider selection, startup preference, taskbar/display settings, refresh interval, and alert state, including quota percentages and reset timestamps |
| `history.jsonl` | Claude session and weekly percentages with timestamps |
| `history-codex.jsonl` | Codex primary and secondary percentages with timestamps |

History is compacted to the most recent eight days when it is loaded. Files can
continue growing while the app stays running. These files are ordinary local
files, without application-level encryption, and use your Windows profile's
access controls. BurnBar does not save provider access tokens in these files.
Older `ClaudeUsageMeter` settings may be migrated into the BurnBar folder.

Usage responses are also held in memory to draw the meter and details panel.
BurnBar's code does not include telemetry, analytics, or cloud synchronization.
Its own direct HTTP usage request is to Anthropic; Codex service traffic is
handled by the installed CLI. Your system's network/proxy settings still apply.

## Diagnostics and shared reports

The `-taskbar-layout` flag prints local taskbar geometry, meter state, and the
foreground process/class information. Usage summaries and error messages can
contain local paths. Diagnostic output is not uploaded automatically.

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
