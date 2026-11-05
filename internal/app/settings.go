package app

import (
	"fmt"
	"github.com/nooblk-98/burnbar/internal/alerts"
	"github.com/nooblk-98/burnbar/internal/updates"
	"slices"
	"strconv"
	"time"
)

const (
	settingPage = 1000 + iota
	settingProviders
	settingInterval
	settingStartup
	settingForecast
	settingHotkey
	settingMilestones
	settingAlerts
	settingPrimary
	settingSecondary
	settingPreset10
	settingPreset25
	settingPresetCritical
	settingQuiet
	settingQuietStart
	settingQuietEnd
	settingWindowsQuiet
	settingDelay
	settingMode
	settingAlways
	settingTheme
	settingScale
	settingUpdates
	settingUpdateStatus
	settingCheckUpdates
	settingReleaseNotes
	settingUpdateCommand
	settingCopyCommand
)

func (a *app) showSettings() {
	if a.settings != nil {
		pShowWindow.Call(a.settings.hwnd, 9)
		pSetForegroundWindow.Call(a.settings.hwnd)
		return
	}
	if a.popupVisible {
		a.hidePopup()
	}
	w := a.newNativeWindow("BurnBar settings", 610, 470)
	if w == nil {
		return
	}
	a.settings = w
	c := a.cfg
	w.label(2000, "&Category:", 20, 18, 90, -1)
	w.combo(settingPage, []string{"General", "Notifications", "Appearance & popups", "Updates"}, 0, 115, 14, 470, -1)
	w.label(2001, "&Usage providers:", 20, 68, 175, 0)
	w.combo(settingProviders, []string{"Claude + Codex", "Claude only", "Codex only"}, modeIndex(c.Providers), 210, 64, 375, 0)
	w.label(2002, "&Refresh interval (minutes):", 20, 108, 185, 0)
	w.edit(settingInterval, strconv.Itoa(c.IntervalMinutes), 210, 104, 80, 0)
	w.check(settingStartup, "Start with &Windows", startupEnabled(), 20, 152, 550, 0)
	w.check(settingForecast, "Show recent-pace &estimates when enough data exists", c.ShowForecast, 20, 192, 550, 0)
	w.check(settingHotkey, "Enable Ctrl+Alt+B to open &readable usage", c.Hotkey, 20, 232, 550, 0)
	w.label(2003, "The readable usage window supports keyboard navigation, text selection,", 20, 280, 565, 0)
	w.label(2004, "copying and Windows screen readers. Open it from the tray menu or F6.", 20, 304, 565, 0)
	w.label(2005, "Estimates stay local and are not guarantees of remaining usage.", 20, 348, 565, 0)

	w.check(settingMilestones, "&Automatic usage milestones", c.MilestonePopups, 20, 60, 280, 1)
	w.check(settingAlerts, "80% / 95% and &reset Windows alerts", c.Alerts, 300, 60, 295, 1)
	w.label(2010, "&Session / primary thresholds (%):", 20, 98, 270, 1)
	w.edit(settingPrimary, alerts.ThresholdText(c.PrimaryThresholds), 20, 122, 565, 1)
	w.label(2011, "&Weekly / secondary thresholds (%):", 20, 160, 285, 1)
	w.edit(settingSecondary, alerts.ThresholdText(c.SecondaryThresholds), 20, 184, 565, 1)
	w.button(settingPreset10, "Every &10%", 20, 222, 150, 1)
	w.button(settingPreset25, "Every &25%", 180, 222, 150, 1)
	w.button(settingPresetCritical, "&50%, 80%, 95%", 340, 222, 245, 1)
	w.label(2012, "Comma-separated values, 1–100. Leave blank to disable that quota.", 20, 260, 565, 1)
	w.check(settingQuiet, "Quiet &hours (local time, 24-hour HH:MM)", c.QuietHours, 20, 290, 330, 1)
	w.label(2013, "&From:", 360, 294, 45, 1)
	w.edit(settingQuietStart, alerts.Clock(c.QuietStart), 410, 290, 65, 1)
	w.label(2014, "&To:", 490, 294, 30, 1)
	w.edit(settingQuietEnd, alerts.Clock(c.QuietEnd), 520, 290, 65, 1)
	w.check(settingWindowsQuiet, "Respect Windows &busy / quiet state and Focus sessions", c.RespectWindowsQuiet, 20, 328, 565, 1)
	w.label(2015, "Suppressed milestones are skipped, not queued. Manual views still work.", 20, 367, 565, 1)

	w.label(2020, "Close automatic and manual views after (&seconds):", 20, 65, 410, 2)
	w.edit(settingDelay, strconv.Itoa(c.PopupSeconds), 465, 61, 120, 2)
	w.label(2021, "&Automatic notice style:", 20, 110, 210, 2)
	mode := 0
	if !c.CompactNotices {
		mode = 1
	}
	if c.NativeNotices {
		mode = 2
	}
	w.combo(settingMode, []string{"Compact card — affected quota only", "Full usage card", "Windows notification — OS-controlled delivery"}, mode, 240, 106, 345, 2)
	w.check(settingAlways, "&Always show overview (optional)", c.AlwaysShowPopup, 20, 156, 565, 2)
	w.label(2022, "&Popup theme:", 20, 207, 185, 2)
	theme := slices.Index([]string{"system", "light", "dark"}, c.Theme)
	w.combo(settingTheme, []string{"Follow Windows", "Light", "Dark"}, max(0, theme), 240, 203, 345, 2)
	w.label(2023, "Popup &text size:", 20, 254, 185, 2)
	w.combo(settingScale, []string{"100%", "125%", "150%"}, max(0, slices.Index([]int{100, 125, 150}, c.TextScale)), 240, 250, 345, 2)
	w.label(2024, "Windows high-contrast colours override the chosen popup theme.", 20, 310, 565, 2)
	w.label(2025, "Use Windows notifications for OS-managed Do Not Disturb delivery.", 20, 340, 565, 2)
	w.label(2026, "Custom cards use shell/Focus checks; standalone DND may not be reported.", 20, 365, 565, 2)

	w.check(settingUpdates, "&Check for stable updates daily", c.CheckUpdates, 20, 64, 565, 3)
	w.label(2030, "Checks public GitHub release metadata only; nothing is installed automatically.", 20, 102, 565, 3)
	w.label(settingUpdateStatus, a.updateStatus(), 20, 146, 565, 3)
	w.button(settingCheckUpdates, "&Check now", 20, 190, 170, 3)
	w.button(settingReleaseNotes, "&Release notes", 210, 190, 180, 3)
	w.label(2031, "&Install/update command (select and copy, or use Copy):", 20, 249, 565, 3)
	w.add(settingUpdateCommand, "EDIT", updates.Command, wsTabStop|0x00800000|0x80|0x800, 20, 275, 565, 26, 3)
	w.button(settingCopyCommand, "C&opy command", 20, 318, 190, 3)
	w.label(2032, "Stable updates are offered even when using development builds,", 20, 365, 565, 3)
	w.label(2033, "but development builds are never automatically switched to stable.", 20, 388, 565, 3)
	w.button(1, "&Save and close", 325, 426, 150, -1)
	w.button(2, "Cancel", 485, 426, 100, -1)
	w.layout(dpiForWindow(a.primaryTaskbar()))
	pShowWindow.Call(w.hwnd, SW_SHOW)
	pSetForegroundWindow.Call(w.hwnd)
	pSetFocus.Call(w.controls[settingPage].hwnd)
}
func modeIndex(mode string) int {
	return max(0, slices.Index([]string{"both", "claude", "codex"}, mode))
}
func (a *app) settingsCommand(id, notification int) {
	w := a.settings
	if w == nil {
		return
	}
	switch id {
	case 1:
		a.saveSettings()
	case 2:
		closeNative(&a.settings)
	case settingPage:
		if notification == 1 {
			w.selectPage(w.selected(settingPage))
		}
	case settingPreset10, settingPreset25, settingPresetCritical:
		values := alerts.DefaultThresholds()
		if id == settingPreset25 {
			values = []int{25, 50, 75, 100}
		}
		if id == settingPresetCritical {
			values = []int{50, 80, 95}
		}
		w.set(settingPrimary, alerts.ThresholdText(values))
		w.set(settingSecondary, alerts.ThresholdText(values))
	case settingCheckUpdates:
		a.checkUpdates(true)
	case settingReleaseNotes:
		a.openURL(a.releaseURL())
	case settingCopyCommand:
		if err := copyText(w.hwnd, updates.Command); err != nil {
			a.settingsError(err)
		} else {
			w.set(settingUpdateStatus, "Update command copied.")
		}
	}
}
func (a *app) settingsError(err error) { a.showMessage("BurnBar settings", err.Error()) }
func parseRange(text, label string, minValue, maxValue int) (int, error) {
	n, e := strconv.Atoi(text)
	if e != nil || n < minValue || n > maxValue {
		return 0, fmt.Errorf("%s must be a whole number from %d to %d", label, minValue, maxValue)
	}
	return n, nil
}
func (a *app) saveSettings() {
	w := a.settings
	if w == nil {
		return
	}
	c := a.cfg
	var err error
	c.PrimaryThresholds, err = alerts.ParseThresholds(w.text(settingPrimary))
	if err != nil {
		a.settingsError(err)
		return
	}
	c.SecondaryThresholds, err = alerts.ParseThresholds(w.text(settingSecondary))
	if err != nil {
		a.settingsError(err)
		return
	}
	c.PopupSeconds, err = parseRange(w.text(settingDelay), "Close delay", 1, 300)
	if err != nil {
		a.settingsError(err)
		return
	}
	c.IntervalMinutes, err = parseRange(w.text(settingInterval), "Refresh interval", 1, 60)
	if err != nil {
		a.settingsError(err)
		return
	}
	c.QuietStart, err = alerts.ParseClock(w.text(settingQuietStart))
	if err != nil {
		a.settingsError(err)
		return
	}
	c.QuietEnd, err = alerts.ParseClock(w.text(settingQuietEnd))
	if err != nil {
		a.settingsError(err)
		return
	}
	providerIndex, themeIndex, scaleIndex, mode := w.selected(settingProviders), w.selected(settingTheme), w.selected(settingScale), w.selected(settingMode)
	if providerIndex < 0 || providerIndex > 2 || themeIndex < 0 || themeIndex > 2 || scaleIndex < 0 || scaleIndex > 2 || mode < 0 || mode > 2 {
		a.settingsError(fmt.Errorf("select a value for each list"))
		return
	}
	c.Providers = []string{"both", "claude", "codex"}[providerIndex]
	c.Theme = []string{"system", "light", "dark"}[themeIndex]
	c.TextScale = []int{100, 125, 150}[scaleIndex]
	c.MilestonePopups = w.checked(settingMilestones)
	c.Alerts = w.checked(settingAlerts)
	c.QuietHours = w.checked(settingQuiet)
	c.RespectWindowsQuiet = w.checked(settingWindowsQuiet)
	c.CompactNotices = mode == 0
	c.NativeNotices = mode == 2
	c.AlwaysShowPopup = w.checked(settingAlways)
	c.ShowForecast = w.checked(settingForecast)
	c.Hotkey = w.checked(settingHotkey)
	c.CheckUpdates = w.checked(settingUpdates)
	on := w.checked(settingStartup)
	c.StartWithWindows = &on
	if !slices.Equal(c.PrimaryThresholds, a.cfg.PrimaryThresholds) || !slices.Equal(c.SecondaryThresholds, a.cfg.SecondaryThresholds) {
		c.MilestoneState = alerts.Tracker{}
		if !a.demo {
			for _, u := range a.snapshot() {
				c.MilestoneState.ObserveThresholds(u, c.PrimaryThresholds, c.SecondaryThresholds)
			}
		}
	}
	acquiredHotkey := c.Hotkey && !a.hotkeyRegistered
	if acquiredHotkey {
		if err := a.registerHotkey(); err != nil {
			a.settingsError(err)
			return
		}
	}
	oldStartup := startupEnabled()
	rollbackHotkey := func() {
		if acquiredHotkey {
			pUnregisterHotKey.Call(a.main, hotkeyID)
			a.hotkeyRegistered = false
		}
	}
	if on != oldStartup {
		if err := setStartup(on); err != nil {
			rollbackHotkey()
			a.settingsError(err)
			return
		}
	}
	if err := c.SaveChecked(); err != nil {
		if on != oldStartup {
			_ = setStartup(oldStartup)
		}
		rollbackHotkey()
		a.settingsError(err)
		return
	}
	if !c.Hotkey && a.hotkeyRegistered {
		pUnregisterHotKey.Call(a.main, hotkeyID)
		a.hotkeyRegistered = false
	}
	oldMode := a.cfg.Providers
	a.cfg = c
	a.hidePopup()
	closeNative(&a.settings)
	a.setRefreshTimer()
	a.updateTray()
	a.syncPopup()
	if c.Providers != oldMode {
		a.refresh(false)
	}
	a.scheduleResetRefresh()
	a.updateAccessible()
	if c.CheckUpdates {
		a.checkUpdates(false)
	}
}
func (a *app) registerHotkey() error {
	r, _, _ := pRegisterHotKey.Call(a.main, hotkeyID, 0x4000|0x0001|0x0002, 'B')
	if r == 0 {
		return fmt.Errorf("Ctrl+Alt+B is already in use. Disable that shortcut in the other app, or leave this option off")
	}
	a.hotkeyRegistered = true
	return nil
}
func (a *app) snooze(minutes int) {
	if minutes < 0 {
		a.cfg.SnoozeUntil = alerts.Tomorrow(time.Now()).Unix()
	} else if minutes == 0 {
		a.cfg.SnoozeUntil = 0
	} else {
		a.cfg.SnoozeUntil = time.Now().Add(time.Duration(minutes) * time.Minute).Unix()
	}
	a.cfg.Save()
	if a.popupLifetime.Automatic() {
		a.hidePopup()
	}
}
