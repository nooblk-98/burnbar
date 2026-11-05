# Settings and everyday use

Right-click the notification icon → **Settings…**. Categories use standard
Windows controls: Tab/Shift+Tab moves focus, Alt+underlined letters activates
controls, Space changes a checkbox, and Escape cancels. **Save and close**
validates every category and persists it; Cancel discards edits. Settings itself
does not auto-close. Existing tray-menu shortcuts remain available.

## Notifications

Session/primary and weekly/secondary milestones have independent comma-separated
threshold lists. Examples: `10,20,30,40,50,60,70,80,90,100`, `25,50,75,100`, or
`50,80,95`. Values must be whole percentages from 1 to 100. Duplicates are removed
and values sorted; an empty list disables milestones for that quota. Preset
buttons set both lists; edit either afterwards to make them different. The lists
apply to both enabled providers, not extra per-model quotas.

Changing thresholds silently establishes current readings as a new baseline;
there is no replay of thresholds already passed. The highest threshold crossed
between reads is shown once per quota. Persisted raw high-water percentages
prevent duplicates after corrections or restart. Disable **Automatic usage
milestones** for manual-only inspection. The 80%/95%/reset Windows alerts have a
separate switch and retain their existing fixed thresholds.

### Quiet controls

**Snooze automatic notices** in the tray menu offers 30 minutes, 1 hour, or until
08:00 tomorrow in your local timezone. **Resume automatic notices** clears snooze
but does not override scheduled quiet hours or Windows suppression. Snooze survives
restart and expires at the saved time.

**Quiet hours** use your local clock, in 24-hour `HH:MM` format. Overnight ranges
such as 22:00–08:00 work across midnight; equal endpoints mean quiet all day.
Quiet controls suppress usage popups, usage balloons, update notifications and
the optional pinned overview. Manual inspection and settings remain available.
Suppressed crossings are consumed, never queued for a burst afterwards.

**Respect Windows quiet state and Focus sessions** is on by default and applies
to BurnBar's custom cards. Cards are suppressed when no fullscreen app is
detected but `SHQueryUserNotificationState` reports a quiet state (not present,
for example locked or screen saver; exclusive D3D; presentation mode; quiet time;
or an immersive fullscreen app), or a supported Windows Focus session is active.
`QUNS_BUSY` alone does not suppress cards, because Windows also reports it outside
fullscreen; actual fullscreen windows are detected separately. If the state query
fails, cards are not suppressed. Focus detection uses the read-only
`Windows.UI.Shell.FocusSessionManager` API, with safe fallback on older Windows.

Windows notifications (the 80%/95%/reset alerts, update notices and the
**Windows notification** milestone style) are sent with Windows' respect-quiet-time
flag, so Windows applies its own quiet time, Focus and Do Not Disturb rules to
them. BurnBar suppresses them only for snooze and scheduled quiet hours.

**Important Windows limitation:** the shell API does not reliably expose every
standalone Do Not Disturb/Focus Assist state on every Windows version. For
OS-controlled delivery, choose **Windows notification** as the automatic notice
style. Windows then controls the banner, duration and suppression according to
its notification settings; BurnBar's custom-card timeout does not control native
notifications. BurnBar does not inspect private registry blobs or change your
Windows Focus/DND settings.

References: [shell notification state](https://learn.microsoft.com/en-us/windows/win32/api/shellapi/nf-shellapi-shqueryusernotificationstate),
[Focus sessions](https://learn.microsoft.com/en-us/windows/apps/windows-app-sdk/applifecycle/focus-session).

## Appearance and popups

- **Compact card** (default): only the quota(s) whose thresholds were just crossed,
  the usage percentage and reset countdown. Click for the full usage details.
- **Full usage card**: affected providers' overview bars plus freshness.
- **Windows notification**: native OS-controlled notices, as described above.
- **Close delay** accepts 1–300 seconds and applies to automatic custom cards,
  manual cards/details and readable usage. The tray menu keeps common presets.
  Background refreshes do not extend a deadline. Deliberate interaction restarts
  the manual/readable timeout; Escape closes sooner.
- **Always show overview** is optional. Quiet/fullscreen suppression still applies;
  timed manual inspection remains available while quiet.
- **Popup theme**: Windows/system, light or dark. Windows high contrast overrides
  these, using system foreground/background colours for text, bars and charts.
- **Text size**: 100%, 125% or 150%, in addition to monitor DPI. The complete popup
  is constrained to the work area. Native settings/readable windows follow DPI
  and system control colours rather than the custom card theme.

## Freshness and estimates

Overview cards display the age of their last successful reading. Errors are
labelled as sign-in required, offline/connection error, rate limited or unavailable;
old values are explicitly marked as last readings. Successful readings older than
max(10 minutes, twice the configured refresh interval) are labelled stale.

**Recent-pace estimates** appear in details/readable usage only when enabled and
there are at least three contiguous observations spanning at least 20 minutes,
with at least one percentage point of growth in the last three hours. Gaps over
20 minutes, corrections/resets, unknown window bounds, errors and readings older
than 10 minutes suppress the estimate. Samples before the reported current cycle
are excluded. Estimates are local, approximate, and assume recent usage continues.
If the projection falls after reset, the view says reset is expected first rather
than suggesting the current quota will run out after its reset.

## Keyboard and readable usage

With a custom card focused:

| Key | Action |
| --- | --- |
| Enter / Space | Open details |
| R | Refresh |
| S | Open settings |
| F6 / Tab | Open readable usage |
| Application/Menu key | Open tray menu |
| Escape / Alt+F4 | Close/collapse |

**Readable usage** is also in the tray menu. It uses a standard read-only text
control with provider names, numeric percentages, health, timestamps, reset times
and eligible forecasts. Select and copy text, use Tab to reach Refresh/Settings/
Close, and use normal Windows screen-reader navigation. No status relies on colour
alone. Its timer uses the selected popup delay; keyboard/mouse-down interaction
restarts it. Choose a longer delay if needed for reading.

Enable the optional **Ctrl+Alt+B** shortcut in General to open readable usage
without finding the tray icon. The shortcut is off by default; conflicts are
reported without stealing another app's registration.

## Updates

Automatic checks are enabled by default, at launch and then at most once every
24 hours while running. Turn them off in Updates; **Check now** still works, with
a one-minute retry limit. Checks fetch only public stable metadata from the
repository's GitHub releases API, with a 15-second timeout. They do not send usage,
provider credentials or account identifiers. Errors/rate limits are shown without
interrupting usage collection.

A newer stable version can produce one quiet-aware Windows notice per version
per run. Development builds show the latest stable in Settings, but are not
compared as stable versions or silently switched. **Release notes** opens the
validated GitHub release URL; **Copy command** copies:

```powershell
npx --yes burnbar@latest update
```

Nothing downloads or installs automatically. Development users can keep their
channel with `npx --yes burnbar@development update` instead.

### When the provider rate-limits usage requests

A 429 response is not your quota percentage. With no successful reading yet,
BurnBar shows an unavailable-data message rather than empty quota bars or a false
"Updated just now". After a previous success, its values and original timestamp
remain visible as last-known data. A retry countdown shows the provider cooldown.
Manual refresh, scheduled polling and activity refresh all respect it. BurnBar
uses exponential backoff (2–16 minutes) and waits longer if a `Retry-After` header
requires it; a dedicated timer retries at that deadline. It cannot remove a
server-side limit, and restarting the app should not be used to force retries.
