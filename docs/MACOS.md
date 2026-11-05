# macOS preview

The macOS port is experimental, initially targeting **macOS 15+** on Apple Silicon
and Intel. It is not a claim of complete Windows feature parity or real-account
verification. No MacBook is needed to build it: standard macOS GitHub Actions
runners compile and exercise synthetic native controls on each PR.

## Install / update

Once a release contains macOS assets, the same installer selects the host:

```sh
npx --yes burnbar@development install
# Stable support starts only with a stable release containing macOS assets:
npx --yes burnbar@latest install
```

Do not expect older npm versions or existing Windows-only releases to install
macOS. Missing assets fail without modifying an installation. You can download
`burnbar-macos-arm64.zip` (Apple Silicon) or `burnbar-macos-amd64.zip` (Intel) from
a compatible release and verify its `.sha256` file with `shasum -a 256`.

The default destination is `~/Applications/BurnBar.app`. `--install-dir` selects
the **parent folder** of BurnBar.app, not the executable. Quit BurnBar before
updating. `--no-start` installs without opening it; `--dry-run` checks metadata
without installing. No sudo, shell-profile edits, automatic login entry or
provider credential writes are performed. Settings/history are kept untouched.

Bundles are **ad-hoc signed**, not Developer ID signed or notarized. This does not
establish Gatekeeper trust. No quarantine attributes or system security settings
are removed/disabled. Developer ID/notarization requires a separate release setup;
the installer will need updating to preserve that signature when introduced.

## Usage

Click the menu-bar **BurnBar** item for numeric quota meters and selectable text
with freshness/errors, reset times and eligible recent-pace forecasts. It closes
after the configured delay. Refresh restarts the manual timer; background refreshes
do not. Right-click for Preferences, refresh, Keychain authorization, snooze for an
hour/resume, update metadata/release notes or Quit.

Preferences use native controls with explicit Save/Cancel, independent threshold
lists, provider selection, polling/close delays, quiet hours, forecasts and update
checks. Preferences do not auto-close. Startup defaults to **manual inspection**;
automatic milestone popovers are an experimental opt-in because macOS Focus and
fullscreen suppression are not implemented yet. Opt-in popovers do not intentionally
activate the app and still respect BurnBar's quiet hours/snooze. Native appearance
follows AppKit system settings. Text can be selected/copied and uses native
accessibility controls; actual VoiceOver behavior still needs beta verification.

The shared Go core retains reset/high-water deduplication, local history, cautious
forecasts, server Retry-After and manual/automatic cooldown protection. Background
work is asynchronous; the AppKit event loop stays on its OS thread. A process lock
prevents concurrent instances from writing the same profile.

## Authentication

- **Claude default profile:** read-only Security.framework lookup of Claude Code's
  `Claude Code-credentials` generic-password item; local credentials file fallback.
  Background reads explicitly disallow authorization UI. Choose **Authorize Claude
  Keychain…** to allow one deliberate access request, then approve the macOS dialog
  if desired. BurnBar never writes/deletes credentials or logs their contents.
- **Custom CLAUDE_CONFIG_DIR:** only that directory's `.credentials.json` is read.
  Profile-specific Keychain service naming is not implemented. BurnBar never falls
  back to the default account when a custom profile lacks a readable file.
- **Codex:** uses the existing local `codex app-server` account/read and
  account/rateLimits/read RPCs, not conversation turns or direct token-file access.
  Finder does not inherit shell PATH, so common Homebrew, `~/.local/bin` and
  `~/.npm-global/bin` locations are appended for this process only. Use
  `BURNBAR_CODEX_PATH` in a terminal launch for another native CLI location.

No real account credentials are placed in CI. Keychain grants may depend on the
binary's signing identity and may need renewed approval after an update. This
must be checked on a real macOS desktop before recommending stable usage.

## Build and automated checks

On macOS with Go, Node.js and Xcode Command Line Tools:

```sh
bash scripts/build-macos.sh 0.0.0-dev
./dist/BurnBar.app/Contents/MacOS/burnbar -demo -once -providers both
APPDATA="$(mktemp -d)" BURNBAR_MACOS_SMOKE=1 \
  ./dist/BurnBar.app/Contents/MacOS/burnbar -demo -providers both
```

The smoke flag requires `-demo` and exits after checking the status item, provider
text, native preferences, Cancel versus Save and timed popover dismissal. APPDATA
isolates the profile; use it to avoid deliberately saving test preferences into
your normal profile. Demo does not read provider credentials, fetch account usage,
write history or advance alert baselines. Explicit preference saves still persist.

Native AppKit builds require cgo/Apple frameworks. Non-cgo builds expose only the
shared diagnostic commands, with an explicit unsupported-desktop error. Windows
code is isolated with build constraints and continues to build independently.

## Remaining beta work

- Real Claude Keychain access/denial/rotation and Codex login/RPC behavior.
- Custom-profile Keychain integration, login items, global shortcuts, Focus and
  native notification integration; pinned overview and graphical history UI.
- Sleep/wake, fullscreen/Spaces, notch/hidden menu bars, monitor changes and Retina.
- Keyboard/VoiceOver interaction and accessibility appearance settings.
- Signed/notarized distribution and clean-machine install/update behavior.

CI execution and synthetic previews do not establish these real-desktop results.
