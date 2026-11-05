# Popup behavior

## Default: milestone notices

BurnBar normally stays in the notification area. On each successful fetch, it
tracks the provider's two main quota windows separately (typically session/5h and
weekly/7d). A new 10% boundary, up through 100%, opens one compact notice without
activating the window. The saved close delay (six seconds by default) applies to
both automatic notices and manually opened views. A 12% → 43% jump shows 40% once, not three
successive notices. More events during an existing notice update it without
extending the original deadline. Only affected quotas are shown in the default compact notice. Full provider cards
are optional in Settings.

The first reading of an untracked window establishes its baseline silently. For
example, starting at 85% does not replay 10–80%; the next notice is at 90%.
High-water state is saved by provider, quota, reset time (minute precision) and
window duration. Downward corrections in the same window do not rearm old levels.
A new reset/window establishes a new baseline; subsequent crossings alert again.
If no reset time is reported, observing usage fall below 10% rearms that window.
Missing, invalid, stale/error or rate-limited readings do not advance this state.
Extra per-model quotas are excluded to avoid multiplying interruptions.

Milestones are detected at fetch time, not the exact instant usage crosses a
boundary. Normal schedule, activity-driven refresh and API backoff still apply.

The tracker advances even if milestone cards are disabled, always-show mode is
selected, a manual view is open, or an app is fullscreen. Those crossings do not
queue a backlog. Fullscreen hides an existing automatic card and cancels it.

## Manual inspection and preferences

- Click the tray icon to open the overview; click the card for full details.
- Clicking an automatic notice promotes it to a timed manual view and restarts the configured delay.
- Manual views close after the same selected delay. Deliberate clicks restart the
  timer; background fetches do not. Clicking elsewhere, Esc or Alt+F4 closes them
  sooner. A stale earlier timer cannot close a newly reopened view prematurely.
- Right-click → **Automatic usage milestones** controls automatic cards.
  Disable it for tray-click-only behavior.
- **Close popup after** offers 3, 5, 6, 10, 15, 30 and 60 seconds; it is saved across
  restarts and applies to all ordinary popup openings.
- **Always show popup (optional)** restores persistent display without a timeout;
  details still time out back to the compact overview in that explicit mode. Disabling it
  hides the card immediately.
- Existing Windows 80%/95%/reset notifications have a separate **Windows alerts**
  switch. The taskbar widget, placement worker, animation and menu settings have been
  removed. Only the notification icon and popups remain.

## Expanded preferences

Settings now provides independent session/weekly thresholds, compact/full/native
notices, 1–300 second close delays, snooze and overnight quiet hours. The same
high-water/reset rules apply to custom thresholds, and suppressed events never
queue. See the [settings guide](SETTINGS.md) for exact behavior, keyboard/readable
views, forecasts, updates and Windows Focus/DND limitations.

## Upgrade

Profiles without `popupBehaviorVersion` use the old always-visible default. On
this upgrade, that setting switches off once and milestone popups default on.
Other preferences and histories are preserved; obsolete widget settings are ignored
and removed when saving. Once the behavior version is saved,
an explicit always-show or milestone opt-out survives future launches.

The native tray tooltip is cleared, including its SHOWTIP flag, while any popup is
visible and restored on hide so it cannot overlap the card. This is independent
of Windows usage-alert balloons.

## Verification limits

Local checks exercise all 10–100% crossings for both windows and both providers,
baselines, reset/unknown-reset rearming, corrections, persisted restart state,
invalid/error readings, independent quotas, and popup deadlines/manual promotion.
The Windows workflow validates build and packaged installation, not interactive
tray, focus, fullscreen or timer behavior on a real desktop. Use a Windows desktop
for final interaction verification; rendered previews are not live screenshots.
