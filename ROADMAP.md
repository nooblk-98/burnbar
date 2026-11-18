# BurnBar roadmap

BurnBar is a cross-platform Rust/Tauri tray app for Claude, Codex, OpenCode Go and
Cursor. Priorities depend on community requests and maintainer time; there are no
promised release dates.

## Available now

- Four usage providers with reported windows, reset times and history.
- Tray-first window on Windows, macOS and Linux from one codebase.
- Milestone cards, 80%/95% and reset alerts, thresholds, snooze and quiet hours.
- 24-hour and 7-day charts, pace markers and cautious forecasts.
- Read-only access to existing local logins, with no telemetry.

## Next priorities

| Work | Status | How to help |
| --- | --- | --- |
| Signed and notarized macOS/Linux packages | Planned | Test the current build and report tray, notification and autostart issues |
| More provider integrations | Accepting requests | [Open an issue](https://github.com/nooblk-98/burnbar/issues) with the provider's usage API or CLI |
| Accessibility and keyboard polish | Ongoing | Report a reproducible issue |
| CI coverage of tray and notification interactions | Planned | Contribute a platform smoke test |

## Provider requests

Include the provider's official usage API or CLI, plan names, the limits you want
to track, and whether you can test the integration. A request is a proposal, not
a support announcement.
