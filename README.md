<div align="center">

<img src="assets/icon.png" alt="BurnBar icon" width="96">

# BurnBar

[![Build](https://github.com/nooblk-98/burnbar/actions/workflows/build.yml/badge.svg)](https://github.com/nooblk-98/burnbar/actions/workflows/build.yml)
[![Release](https://img.shields.io/github/v/release/nooblk-98/burnbar?include_prereleases&label=release)](https://github.com/nooblk-98/burnbar/releases)
[![Go](https://img.shields.io/github/go-mod/go-version/nooblk-98/burnbar)](go.mod)
[![Platform](https://img.shields.io/badge/platform-Windows%2010%20%7C%2011-0078D4)](#installation)
[![License](https://img.shields.io/github/license/nooblk-98/burnbar)](LICENSE)

**Claude and Codex usage at a glance, beside your Windows tray.**<br>
See how fast you're burning through your plan limits without leaving what you're doing.

[Features](#features) • [See it in action](#see-it-in-action) • [Installation](#installation) • [Build from source](#build-from-source)

<img src="docs/images/milestone-popup-dark.png" alt="BurnBar brief usage milestone notification" width="410">

</div>

Claude and Codex have session and longer-term plan limits. BurnBar keeps both providers in a tray-anchored card when you need it, warns you before you run out, and shows whether you're using them faster than the time
window allows. Use both together, or select one from **Usage providers** in the right-click menu.
Codex window lengths come from your account rather than assuming every plan uses the same limits.

> [!NOTE]
> BurnBar is an unofficial community project, not affiliated with or endorsed by Anthropic or OpenAI.
> "Claude" and "Codex" belong to their respective owners.

## Features

- **Brief milestone popups**: by default, a small framed card appears when either
  provider's primary/session or secondary/weekly quota crosses **10%, 20%, … 100%**.
  It shows the affected provider, takes no focus, and closes after the selected delay (**6 seconds** by default).
  More updates do not keep extending it. Jumps show only the highest new milestone,
  not a burst of every skipped threshold.
- **Quiet by default**: first readings establish a baseline instead of replaying
  old milestones. Saved per-window state avoids duplicates on refresh or restart;
  quota resets rearm the tracker. Crossings are detected on the next successful
  refresh, not continuously between API reads.
- **Your choice**: click the tray icon any time to inspect usage; click an automatic
  notice for details. Manual popups also close after the selected delay; clicks restart
  their timer. Right-click → **Milestone popups (10% steps)** disables automatic
  cards for click-only use. **Always show popup (optional)**
  restores a persistent overview if preferred. Esc or clicking elsewhere closes
  manual views unless always-show is enabled.
- **Fullscreen-aware**: no automatic popup appears over a fullscreen app, and missed
  milestones are not queued to interrupt you afterwards. Cards follow light/dark
  theme, DPI and the notification icon, and remain inside the monitor's work area.
- **Configurable close delay**: **Close popup after** applies to both automatic and
  manually opened views: 3, 5, 6, 10, 15, 30 or 60 seconds. Your selection survives
  restarts. Background usage refreshes do not extend the timer.
- **No tooltip overlap**: the native tray tooltip is hidden while a popup is visible
  and restored once it closes.
- **Pace marker**: a tick on each bar shows how much of the window's time has passed.
  A bar past its tick means you'll hit the limit before it resets.
- **Details panel**: separate provider panels, reported quota windows, reset times, and independent
  24-hour and 7-day history charts. Claude also shows this week's usage by product when available.
- **Alerts**: Windows notifications at 80% and 95%, and when a limit you'd been close to resets.
- **Fresh numbers**: refreshes on a schedule, within a minute of Claude Code or Codex activity, and right after
  a limit resets, backing off automatically if the API rate-limits.
- **Tray-only**: no taskbar widget or overlay windows. The popup follows the primary
  notification icon and remains inside that monitor's work area.
- **Lightweight**: one native executable, installed or updated with npx. Codex usage uses your existing Codex CLI.
- **Start with Windows**: enabled on first launch for new users; turn it off from the
  right-click menu at any time. Existing startup preferences are preserved.
- **About**: right-click the popup or tray icon for the app version, developer credits,
  and a link to this repository.

Existing installations using the previous always-visible default switch to the
quieter milestone mode once on upgrade. You can opt back into always-show after
updating; that explicit choice is retained. See [popup behavior](docs/POPUP_BEHAVIOR.md).

## See it in action

<div align="center">
<img src="docs/images/milestone-popup-light.png" alt="BurnBar milestone popup, light theme" width="400">
<img src="docs/images/milestone-popup-dark.png" alt="BurnBar milestone popup, dark theme" width="400">
<br>
<sub>Rendered demo previews: a milestone notice that closes automatically. Click to inspect details.</sub>
</div>

## Installation

You need Windows 10 or 11 and a signed-in provider:

- **Claude:** [Claude Code](https://claude.com/claude-code) signed in with a Claude subscription.
- **Codex:** [Codex CLI](https://developers.openai.com/codex/cli/) signed in with **ChatGPT**
  (`codex login`). API-key-only accounts do not provide these subscription usage limits.
  BurnBar also detects the CLI bundled with the Windows Codex desktop app when available.

Both providers are available from the tray; automatic notices show the affected provider. A missing login affects only that provider. Select
**Usage providers** from the right-click menu to hide a provider you don't use.

### Install or update with npx

Requires [Node.js 22+](https://nodejs.org/) (includes npm/npx) and Windows PowerShell
5.1. BurnBar itself does not need Node.js after installation. No administrator
access or npm login is required.

```powershell
# Install or update to the latest development build
npx --yes burnbar@development install

# Install or update to the latest stable executable
npx --yes burnbar@development install --channel stable
```

`burnbar@development` is published and verified. Use the explicit stable-channel
command above until the first stable npm package is released from main. npm's
initial `latest` alias currently also points to a development installer; do not
use that alias to select stable software yet.

After the first stable version tag publishes the stable npm channel, this will
become the stable shortcut:

```powershell
npx --yes burnbar@latest install
```

`install` and `update` perform the same verified process. Running the command
again updates the executable; there is no background auto-updater. Switching
channels preserves settings, history and startup preferences.

```powershell
# Optional: another folder, without launching
npx --yes burnbar@development update --channel stable --install-dir 'D:\Apps\BurnBar' --no-start

# Check release metadata without installing
npx --yes burnbar@development install --dry-run
```

Both channels install into `%LOCALAPPDATA%\Programs\BurnBar` by default, verify
SHA-256 before replacement, and create a Start menu shortcut. The reviewed Windows
helper is bundled inside the npm package; no remote script is fetched and executed.
`--dry-run` changes no files or running processes. Development builds may contain
bugs; stable installs exclude prereleases. See [release setup](docs/RELEASING.md).

On its first normal launch, BurnBar enables **Start with Windows** for a new profile.
You can disable it from the right-click menu. Demo and diagnostic modes do not enable it.

> [!NOTE]
> BurnBar is distributed **unsigned**. Windows Smart App Control may block it,
> and SmartScreen may show *Windows protected your PC*.

### If Smart App Control blocks BurnBar (Windows 11)

> [!WARNING]
> Turning off Smart App Control reduces protection for your whole PC. Recent Windows
> updates support turning it back on, but availability depends on your device and
> configuration. If Windows warns that restoring it requires a reset or reinstall,
> do not proceed expecting a temporary change.

1. Install the latest Windows updates before changing this setting.
2. Open **Windows Security > App & browser control > Smart App Control settings**.
3. If you trust the download and choose to proceed, select **Off** before running
   BurnBar. Read the confirmation carefully.
4. Run the npx install command above again, then launch BurnBar.
5. Afterwards, return to **Smart App Control settings** and select **On**, if
   available. Restart if Windows asks, and confirm protection is on.

Turning protection back on may block BurnBar again. Running it once does not create
an exception for the app. See [Microsoft's Smart App Control FAQ](https://support.microsoft.com/en-us/windows/security/threat-malware-protection/smart-app-control-frequently-asked-questions)
for current requirements and limitations.

## Build from source

You need [Go 1.26+](https://go.dev/dl/) on Windows.

```powershell
git clone https://github.com/nooblk-98/burnbar.git
cd burnbar
.\build.ps1
.\bin\burnbar.exe
```

`build.ps1` runs vet and any locally available tests, then builds `bin\burnbar.exe`.
Add `-Resources` to regenerate the icon, manifest and version info, or `-SkipTests`
to skip local tests. Test sources are excluded from Git.

The executable also accepts a few flags for development:

| Flag | Description |
|---|---|
| `-demo` | Show sample numbers and history without calling the API |
| `-providers both` | Show Claude and Codex; also accepts `claude` or `codex`. Defaults to the saved menu setting. |
| `-snapshot <dir>` | Render popup PNG previews in both themes, then exit. Combine with `-demo -providers both`. |
| `-once` | Print usage summaries for the selected providers, then exit. Needs a console build: `go build -o bin\burnbar-cli.exe ./cmd/burnbar` |

> [!WARNING]
> Local builds and release builds are unsigned, so Windows **Smart App Control** may block them.
> See [Releasing BurnBar](docs/RELEASING.md) for the release process.

<details>
<summary><strong>Project layout</strong></summary>

```
cmd/burnbar/          entry point, flags, embedded icon and manifest
internal/app/         Windows UI: tray icon, timed popups, details panel, menus and Win32
internal/usage/       Claude usage client and Codex app-server account client
internal/render/      drawing the meter, details panel, charts and tray icon
internal/layout/      popup placement and fullscreen visibility
internal/config/      settings and alert state
internal/alerts/      milestone deduplication and popup lifetime
internal/history/     usage history for the charts
tools/genres/         icon and Windows resource generator
assets/               app icon
docs/                 screenshots and release guide
.github/workflows/    CI, GitHub releases and optional winget publishing
```

</details>

## Contributing

Contributors are welcome! Bug fixes, documentation improvements, and new AI tool
integrations are all appreciated. Feel free to open a pull request.

See the [contribution guide](CONTRIBUTING.md) for setup and checks, the
[roadmap](ROADMAP.md) for upcoming work, and [privacy details](PRIVACY.md) for data
access and storage.

Support for more AI tools is coming soon. To request a specific tool or suggest a
feature, [open an issue](https://github.com/nooblk-98/burnbar/issues/new/choose) and tell us
what you would like to see.
