package app

import (
	"fmt"
	"image"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/nooblk-98/burnbar/internal/alerts"
	"github.com/nooblk-98/burnbar/internal/config"
	"github.com/nooblk-98/burnbar/internal/history"
	"github.com/nooblk-98/burnbar/internal/layout"
	"github.com/nooblk-98/burnbar/internal/render"
	"github.com/nooblk-98/burnbar/internal/updates"
	"github.com/nooblk-98/burnbar/internal/usage"

	"golang.org/x/sys/windows/registry"
)

const (
	msgUpdate     = WM_APP + 8
	msgUsage      = WM_APP + 1 // background fetch finished
	msgTray       = WM_APP + 2 // notification-area icon callback
	msgMenu       = WM_APP + 3 // popup asks for the context menu
	msgActivity   = WM_APP + 4 // a provider wrote to its session logs
	msgShellEvent = WM_APP + 6 // deferred WinEvent hook processing
	msgTrayAction = WM_APP + 7 // deferred tray click/menu processing

	timerPopupLayout = 1 // 1s: fullscreen, tray position and theme check
	timerRefresh     = 2 // regular usage refresh
	timerReset       = 4 // just after the next limit resets
	timerActive      = 5 // deferred refresh for providers with session activity
	timerPopup       = 8 // auto-dismiss milestone notices

	cmdSettings       = 12
	cmdAccessible     = 13
	cmdSnooze30       = 14
	cmdSnooze60       = 15
	cmdSnoozeTomorrow = 16
	cmdResume         = 17
	cmdDetails        = 1
	cmdRefresh        = 2
	cmdAlerts         = 5
	cmdStartup        = 6
	cmdAbout          = 7
	cmdRepository     = 8
	cmdExit           = 9
	cmdAlwaysShow     = 10
	cmdMilestones     = 11
	cmdPopupDelay     = 400 // + seconds
	cmdInterval       = 200 // + minutes
	cmdProvider       = 300 // + index into providerModes
	activeMinGap      = 60 * time.Second

	runKey         = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue       = "BurnBar"
	legacyRunValue = "ClaudeUsageMeter" // pre-rename builds
)

var (
	popupDelays   = []int{3, 5, 6, 10, 15, 30, 60}
	intervals     = []int{1, 3, 5, 10}
	providerModes = []struct{ key, label string }{{"both", "Claude + Codex"}, {"claude", "Claude only"}, {"codex", "Codex only"}}
)

type app struct {
	settings, accessible          *nativeWindow
	accessibleDeadline            time.Time
	popupEvents                   []alerts.Milestone
	popupTheme                    render.Theme
	updateBusy                    bool
	lastUpdateCheck               time.Time
	latestRelease, pendingRelease updates.Release
	pendingUpdateError            error
	updateError, updateNotified   string
	focus                         *focusReader
	hotkeyRegistered              bool

	demo    bool
	version string
	cfg     config.Config
	hist    map[usage.Provider]*history.History

	hinst uintptr
	main  uintptr
	popup uintptr

	mu       sync.Mutex
	readings map[usage.Provider]usage.Usage
	fetches  map[usage.Provider]*fetchState

	popupLifetime  alerts.PopupLifetime
	popupNotice    string
	popupProviders map[usage.Provider]bool
	popupAnchor    RECT
	popupWork      RECT
	popupScale     float64
	popupLight     bool
	popupExpanded  bool
	popupVisible   bool
	popupActive    bool // became the foreground window at least once since shown
	popupHiddenAt  time.Time
	popupTB        uintptr
	popupMinute    int

	trayIcon       uintptr
	trayAdded      bool
	trayVersion4   bool
	trayInput      trayInput
	taskbarCreated uint32
}

type fetchState struct {
	fetching atomic.Bool
	// Remaining fields belong to the main thread.
	cooldownUntil time.Time
	backoff       time.Duration
	lastFetch     time.Time
	activePending bool
}

func providerID(p usage.Provider) uintptr {
	if p == usage.Codex {
		return 1
	}
	return 0
}

func providerFromID(id uintptr) usage.Provider {
	if id == 1 {
		return usage.Codex
	}
	return usage.Claude
}

func (a *app) enabled(p usage.Provider) bool {
	for _, enabled := range usage.Providers(a.cfg.Providers) {
		if p == enabled {
			return true
		}
	}
	return false
}

var theApp *app

// Callbacks must stay referenced for the life of the process.
var (
	mainProcCB  = windowsCallback(mainProc)
	popupProcCB = windowsCallback(popupProc)
	winEventCB  = windowsCallback7(winEventProc)
)

func (a *app) run() error {
	// Shell browser actions use the GUI apartment.
	if hr, _, _ := pCoInitializeEx.Call(0, coinitApartmentThreaded); hr == 0 || hr == 1 {
		defer pCoUninitialize.Call()
	}
	pSetProcessDpiAwarenessContext.Call(DPI_PER_MONITOR_V2)
	a.hinst, _, _ = pGetModuleHandleW.Call(0)
	cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	icon, _, _ := pLoadImageW.Call(a.hinst, uintptr(unsafe.Pointer(utf16("APP"))), IMAGE_ICON, 0, 0, LR_DEFAULTSIZE)

	for _, c := range []struct {
		name string
		proc uintptr
	}{{"BurnBarMain", mainProcCB}, {"BurnBarPopup", popupProcCB}, {"BurnBarControls", controlsProcCB}} {
		wc := WNDCLASSEXW{LpfnWndProc: c.proc, HInstance: a.hinst, HCursor: cursor, HIcon: icon, LpszClassName: utf16(c.name)}
		if c.name == "BurnBarControls" {
			wc.HbrBackground = 16
		}
		wc.CbSize = uint32(unsafe.Sizeof(wc))
		if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
			return fmt.Errorf("RegisterClassEx %s: %v", c.name, err)
		}
	}

	// Hidden owner window: receives tray callbacks, timers and menu commands.
	a.main, _, _ = pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(utf16("BurnBarMain"))),
		uintptr(unsafe.Pointer(utf16("BurnBar"))), WS_POPUP, 0, 0, 0, 0, 0, 0, a.hinst, 0)
	a.popup, _, _ = pCreateWindowExW.Call(WS_EX_LAYERED|WS_EX_TOOLWINDOW|WS_EX_TOPMOST,
		uintptr(unsafe.Pointer(utf16("BurnBarPopup"))), uintptr(unsafe.Pointer(utf16("BurnBar details"))),
		WS_POPUP, 0, 0, 0, 0, 0, 0, a.hinst, 0)
	if a.main == 0 || a.popup == 0 {
		return fmt.Errorf("could not create windows")
	}

	a.focus = newFocusReader()
	defer a.focus.close()
	if a.cfg.Hotkey {
		if err := a.registerHotkey(); err != nil {
			a.showMessage("BurnBar shortcut", err.Error())
		}
	}
	defer pUnregisterHotKey.Call(a.main, hotkeyID)
	defer closeNative(&a.settings)
	defer closeNative(&a.accessible)
	startupErr := a.initializeStartup()
	r, _, _ := pRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(utf16("TaskbarCreated"))))
	a.taskbarCreated = uint32(r)
	pSetWinEventHook.Call(EVENT_SYS_FOREGND, EVENT_SYS_FOREGND, 0, winEventCB, 0, 0, WINEVENT_OUTOFCTX)
	if !a.demo {
		for _, p := range usage.Providers("both") {
			watchActivity(p, func() { pPostMessageW.Call(a.main, msgActivity, providerID(p), 0) })
		}
	}

	a.updateTray()
	a.syncPopup()
	if startupErr != nil {
		a.notify("Start with Windows could not be updated", startupErr.Error(), true)
	}
	a.refresh(true)
	pSetTimer.Call(a.main, timerPopupLayout, 1000, 0)
	a.setRefreshTimer()
	a.checkUpdates(false)

	var m MSG
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		if a.dialogMessage(&m) {
			continue
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	a.removeTray()
	return nil
}

func (a *app) snapshot() []usage.Usage {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []usage.Usage
	for _, p := range usage.Providers(a.cfg.Providers) {
		out = append(out, a.readings[p])
	}
	return out
}

func (a *app) interval() time.Duration { return time.Duration(a.cfg.IntervalMinutes) * time.Minute }

func (a *app) setRefreshTimer() {
	pSetTimer.Call(a.main, timerRefresh, uintptr(a.interval().Milliseconds()), 0)
}

func (a *app) oneShot(id uintptr, d time.Duration) {
	pSetTimer.Call(a.main, id, uintptr(max(d, 100*time.Millisecond).Milliseconds()), 0)
}

// Fetching

// refresh starts a background fetch. Automatic refreshes respect the rate-limit back-off.
func (a *app) refresh(manual bool) {
	for _, p := range usage.Providers(a.cfg.Providers) {
		a.refreshProvider(p, manual)
	}
}

func (a *app) refreshProvider(p usage.Provider, manual bool) {
	state := a.fetches[p]
	if !state.beginFetch(time.Now(), manual) {
		return
	}
	if a.demo {
		a.mu.Lock()
		a.readings[p] = usage.DemoProvider(p)
		a.mu.Unlock()
		state.fetching.Store(false)
		pPostMessageW.Call(a.main, msgUsage, providerID(p), 0)
		return
	}
	go func() {
		u := usage.FetchProvider(p)
		a.mu.Lock()
		if u.Err != "" {
			// Keep showing the last good numbers; the tooltip and popup report the error.
			prev := a.readings[p]
			u.FiveHour, u.SevenDay, u.Extra, u.Breakdown = prev.FiveHour, prev.SevenDay, prev.Extra, prev.Breakdown
			if len(prev.All()) > 0 {
				u.UpdatedAt = prev.UpdatedAt
			}
		}
		a.readings[p] = u
		a.mu.Unlock()
		state.fetching.Store(false)
		pPostMessageW.Call(a.main, msgUsage, providerID(p), 0)
	}()
}

func (a *app) onUsage(p usage.Provider) {
	if !a.enabled(p) {
		return
	}
	a.mu.Lock()
	u := a.readings[p]
	a.mu.Unlock()
	state := a.fetches[p]
	switch {
	case u.RateLimited:
		state.backoff = min(max(2*state.backoff, 2*time.Minute), 16*time.Minute)
		state.cooldownUntil = time.Now().Add(state.backoff)
	case u.Err == "":
		state.backoff = 0
		state.cooldownUntil = time.Time{}
		if !a.demo {
			a.hist[p].Add(u)
			a.checkAlerts(u)
			a.checkMilestones(u)
		}
	}
	a.scheduleResetRefresh()
	a.updateAccessible()
	a.updateTray()
	if a.popupVisible {
		a.renderPopup()
	}
}

// scheduleResetRefresh fetches again right after the next limit resets, so the bar drops at once.
func (a *app) scheduleResetRefresh() {
	pKillTimer.Call(a.main, timerReset)
	var next time.Time
	for _, u := range a.snapshot() {
		if u.Err != "" {
			continue
		}
		for _, w := range u.All() {
			if w.ResetsAt.After(time.Now()) && (next.IsZero() || w.ResetsAt.Before(next)) {
				next = w.ResetsAt
			}
		}
	}
	if d := time.Until(next) + 15*time.Second; !next.IsZero() && d < a.interval() {
		a.oneShot(timerReset, d)
	}
}

// Alerts

func (a *app) checkAlerts(u usage.Usage) {
	changed := false
	for _, w := range u.All() {
		cycle := w.ResetsAt.Truncate(time.Minute).Unix()
		key := w.Key // retain existing Claude alert state
		if u.Provider == usage.Codex {
			key = "codex/" + key
		}
		prev, seen := a.cfg.AlertState[key]
		cur := config.AlertState{ResetsAt: cycle, Percent: w.Percent}
		if seen && prev.ResetsAt == cycle {
			cur.Level = prev.Level
		}
		if seen && prev.ResetsAt != cycle && prev.Level > 0 && w.Percent < prev.Percent && a.cfg.Alerts && !a.automaticSuppressed() {
			a.notify(u.Name()+" "+w.Label+" limit has reset", fmt.Sprintf("You're back to %.0f%% used.", w.Percent), false)
		}
		level := 0
		if w.Percent >= 95 {
			level = 95
		} else if w.Percent >= 80 {
			level = 80
		}
		if level > cur.Level {
			if a.cfg.Alerts && !a.automaticSuppressed() {
				a.notify(fmt.Sprintf("%s %s at %.0f%%", u.Name(), w.Label, w.Percent), render.ResetText(w.ResetsAt), true)
			}
			cur.Level = level
		}
		if cur != prev {
			a.cfg.AlertState[key] = cur
			changed = true
		}
	}
	if changed {
		a.cfg.Save()
	}
}

func (a *app) notify(title, body string, warn bool) {
	nid := NOTIFYICONDATAW{HWnd: a.main, UID: 1, UFlags: NIF_INFO, DwInfoFlags: NIIF_INFO | NIIF_RESPECT_QUIET_TIME}
	if warn {
		nid.DwInfoFlags = NIIF_WARNING | NIIF_RESPECT_QUIET_TIME
	}
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	copyUTF16(nid.SzInfoTitle[:], title)
	copyUTF16(nid.SzInfo[:], body)
	pShellNotifyIconW.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&nid)))
}

// Popup rendering

func systemUsesLightTheme() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("SystemUsesLightTheme")
	return err == nil && v == 1
}

func (a *app) paint(hwnd uintptr, img *image.RGBA, x, y int) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	screen, _, _ := pGetDC.Call(0)
	defer pReleaseDC.Call(0, screen)
	mem, _, _ := pCreateCompatibleDC.Call(screen)
	defer pDeleteDC.Call(mem)
	hbm, bits := newDIB(w, h)
	if hbm == 0 {
		return
	}
	defer pDeleteObject.Call(hbm)
	render.ToBGRA(img, bits)
	old, _, _ := pSelectObject.Call(mem, hbm)
	defer pSelectObject.Call(mem, old)

	dst := POINT{int32(x), int32(y)}
	size := SIZE{int32(w), int32(h)}
	src := POINT{}
	blend := BLENDFUNCTION{SourceConstantAlpha: 255, AlphaFormat: AC_SRC_ALPHA}
	pUpdateLayeredWindow.Call(hwnd, screen, uintptr(unsafe.Pointer(&dst)), uintptr(unsafe.Pointer(&size)),
		mem, uintptr(unsafe.Pointer(&src)), 0, uintptr(unsafe.Pointer(&blend)), ULW_ALPHA)
}

// Details popup

// togglePopup is called only from deferred UI work, never a shell callback.
func (a *app) togglePopup() {
	tb := a.primaryTaskbar()
	if tb == 0 {
		return
	}
	if a.popupLifetime.Automatic() {
		a.popupTB = tb
		a.showPopup(true) // inspecting a notice promotes it to a manual view
		return
	}
	if a.cfg.AlwaysShowPopup {
		a.popupExpanded = !a.popupExpanded
		if a.popupExpanded {
			a.manualPopup()
		} else {
			a.clearPopup()
		}
		if a.popupVisible {
			a.renderPopup()
		} else {
			a.syncPopup()
		}
		return
	}
	if a.popupVisible {
		a.hidePopup()
		return
	}
	if time.Since(a.popupHiddenAt) < 300*time.Millisecond {
		return
	}
	a.popupTB = tb
	a.popupExpanded = false
	a.showPopup(true)
}

func (a *app) showPopup(activate bool) {
	if activate {
		a.manualPopup()
	}
	a.renderPopup()
	if !a.popupVisible {
		a.popupActive = false
	}
	a.popupVisible = true
	a.updateTrayTooltip()
	if activate {
		pShowWindow.Call(a.popup, SW_SHOW)
		pSetForegroundWindow.Call(a.popup)
	} else {
		pShowWindow.Call(a.popup, SW_SHOWNOACTIVATE)
	}
}

func (a *app) hidePopup() {
	a.clearPopup()
	a.popupVisible = false // ShowWindow may synchronously deliver WM_ACTIVATE
	a.popupHiddenAt = time.Now()
	pShowWindow.Call(a.popup, SW_HIDE)
	a.updateTrayTooltip()
}

// syncPopup restores a persistent card without stealing focus after fullscreen,
// Explorer restart, theme/DPI changes, or moving the notification icon.
func (a *app) syncPopup() {
	if fullscreenAppActive() || a.cfg.AlwaysShowPopup && a.popupLifetime.Deadline.IsZero() && a.automaticSuppressed() || a.popupLifetime.Automatic() && a.automaticSuppressed() {
		if a.popupVisible {
			a.hidePopup()
		}
		return
	}
	if a.cfg.AlwaysShowPopup && a.settings == nil && a.accessible == nil {
		a.popupTB = a.primaryTaskbar()
		if a.popupTB == 0 {
			if a.popupVisible {
				a.hidePopup()
			}
			return
		}
		if !a.popupVisible {
			a.showPopup(false)
		} else if a.popupChanged() {
			a.renderPopup()
		}
	} else if a.popupVisible && a.popupChanged() {
		a.renderPopup()
	}
}

func (a *app) clearPopup() {
	a.popupLifetime.Clear()
	a.popupEvents = nil
	a.popupNotice = ""
	a.popupProviders = nil
	pKillTimer.Call(a.main, timerPopup)
}

func (a *app) popupDelay() time.Duration { return time.Duration(a.cfg.PopupSeconds) * time.Second }

func (a *app) manualPopup() {
	a.clearPopup()
	a.popupLifetime.StartManual(time.Now(), a.popupDelay())
	a.oneShot(timerPopup, a.popupLifetime.Remaining(time.Now()))
}

func (a *app) expirePopup() {
	if a.popupLifetime.Deadline.IsZero() {
		return
	}
	now := time.Now()
	if a.popupLifetime.Due(now) {
		if a.cfg.AlwaysShowPopup {
			a.dismissPopup()
		} else {
			a.hidePopup()
		}
	} else {
		a.oneShot(timerPopup, a.popupLifetime.Remaining(now))
	}
}

func (a *app) checkMilestones(u usage.Usage) {
	events, changed := a.cfg.MilestoneState.ObserveThresholds(u, a.cfg.PrimaryThresholds, a.cfg.SecondaryThresholds)
	if changed && !a.demo {
		a.cfg.Save()
	}
	if len(events) == 0 || !a.cfg.MilestonePopups || a.cfg.AlwaysShowPopup || a.automaticSuppressed() {
		return
	}
	if a.settings != nil || a.accessible != nil {
		return
	}
	if a.cfg.NativeNotices {
		var lines []string
		for _, e := range events {
			lines = append(lines, fmt.Sprintf("%s %s: %.0f%% used (%d%% crossed). %s", e.Provider.Name(), e.Label, e.Percent, e.Level, render.ResetText(e.ResetsAt)))
		}
		a.notify("BurnBar usage milestone", strings.Join(lines, "\n"), false)
		return
	}
	// Never replace the view the user is inspecting.
	if a.popupVisible && !a.popupLifetime.Automatic() {
		return
	}
	tb := a.primaryTaskbar()
	if tb == 0 {
		return
	}
	a.popupTB = tb
	if a.popupProviders == nil {
		a.popupProviders = map[usage.Provider]bool{}
	}
	var notices []string
	for _, event := range events {
		a.popupProviders[event.Provider] = true
		notices = append(notices, fmt.Sprintf("%s %s crossed %d%%", event.Provider.Name(), event.Label, event.Level))
	}
	a.popupNotice = strings.Join(notices, " · ")
	a.popupEvents = events
	a.popupExpanded = false
	a.popupLifetime.StartAutomatic(time.Now(), a.popupDelay())
	a.showPopup(false)
	a.oneShot(timerPopup, a.popupLifetime.Remaining(time.Now()))
}

func (a *app) popupChanged() bool {
	return a.popupAnchor != a.trayAnchor() || a.popupWork != workArea(a.popupTB) ||
		a.popupScale != a.renderScale() || a.popupTheme != a.theme() ||
		a.popupMinute != time.Now().Minute()
}

// dismissPopup collapses details in always-visible mode, or closes a click-only card.
func (a *app) dismissPopup() {
	if a.cfg.AlwaysShowPopup {
		a.clearPopup()
		a.popupExpanded = false
		a.renderPopup()
	} else {
		a.hidePopup()
	}
}

func (a *app) trayAnchor() RECT {
	tb, _ := windowRect(a.popupTB)
	// NOTIFYICONIDENTIFIER's native pointer alignment is provided by Go.
	id := struct {
		Size uint32
		Hwnd uintptr
		UID  uint32
		GUID [16]byte
	}{Hwnd: a.main, UID: 1}
	id.Size = uint32(unsafe.Sizeof(id))
	var icon RECT
	if hr, _, _ := pShellNotifyIconGetRect.Call(uintptr(unsafe.Pointer(&id)), uintptr(unsafe.Pointer(&icon))); hr == 0 && icon.Right > icon.Left && icon.Bottom > tb.Top && icon.Top < tb.Bottom && icon.Right > tb.Left && icon.Left < tb.Right {
		return icon
	}
	// Hidden overflow icons have no useful taskbar rectangle. Anchor near the
	// primary notification area, not at the cursor or the hidden overflow flyout.
	scale := float64(dpiForWindow(a.popupTB)) / 96
	x := tb.Right - int32(60*scale)
	return RECT{Left: x, Right: x + 1, Top: tb.Top, Bottom: tb.Bottom}
}

func (a *app) renderPopup() {
	if a.popupTB == 0 {
		a.popupTB = a.primaryTaskbar()
	}
	if a.popupTB == 0 {
		return
	}
	scale := a.renderScale()
	readings := a.snapshot()
	if a.popupLifetime.Automatic() {
		filtered := make([]usage.Usage, 0, len(readings))
		for _, u := range readings {
			if a.popupProviders[u.Provider] {
				filtered = append(filtered, u)
			}
		}
		readings = filtered
	}
	work, anchor := workArea(a.popupTB), a.trayAnchor()
	th := a.theme()
	var content *image.RGBA
	if a.popupExpanded {
		var panels []render.Panel
		for _, u := range readings {
			panels = append(panels, render.Panel{Usage: u, History: a.hist[u.Provider].Since(time.Now().Add(-7 * 24 * time.Hour)), ShowForecast: a.cfg.ShowForecast, StaleAfter: a.staleAfter()})
		}
		content = render.GroupPopup(scale, panels, th, int(work.Right-work.Left)-24, int(work.Bottom-work.Top)-24)
	} else if a.popupLifetime.Automatic() && a.cfg.CompactNotices {
		content = render.CompactNotice(scale, a.popupEvents, th)
	} else if a.popupLifetime.Automatic() {
		content = render.TrayMilestoneCard(scale, readings, th, a.popupNotice)
	} else if a.cfg.AlwaysShowPopup {
		content = render.TrayPinnedStatusCard(scale, readings, th, a.staleAfter())
	} else {
		content = render.TrayStatusCard(scale, readings, th, a.staleAfter())
	}
	maxW, maxH := int(work.Right-work.Left), int(work.Bottom-work.Top)
	if maxW <= 0 || maxH <= 0 {
		return
	}
	tailTop := anchor.Bottom <= work.Top
	img := render.TrayBubble(content, scale, th, content.Rect.Dx()/2, tailTop)
	img = render.FitPopup(img, maxW, maxH)
	bounds, _ := layout.PlacePopup(work, anchor, img.Rect.Dx(), img.Rect.Dy(), int(6*scale))
	// Point toward the actual icon after clamping the card to the screen edge.
	tipX := int((anchor.Left+anchor.Right)/2 - bounds.Left)
	tipX = tipX * (content.Rect.Dx() + 2*max(2, int(4*scale))) / img.Rect.Dx()
	img = render.FitPopup(render.TrayBubble(content, scale, th, tipX, tailTop), maxW, maxH)
	a.paint(a.popup, img, int(bounds.Left), int(bounds.Top))
	a.popupAnchor, a.popupWork = anchor, work
	a.popupScale, a.popupLight = scale, systemUsesLightTheme()
	a.popupTheme = th
	a.popupMinute = time.Now().Minute()
}

func (a *app) primaryTaskbar() uintptr {
	tb, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(utf16("Shell_TrayWnd"))), 0)
	return tb
}

// Notification-area icon

func makeIcon(img *image.RGBA) uintptr {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	color, bits := newDIB(w, h)
	if color == 0 {
		return 0
	}
	defer pDeleteObject.Call(color)
	render.ToBGRA(img, bits)
	mask, _, _ := pCreateBitmap.Call(uintptr(w), uintptr(h), 1, 1, 0)
	defer pDeleteObject.Call(mask)
	ii := ICONINFO{FIcon: 1, HbmMask: mask, HbmColor: color}
	icon, _, _ := pCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	return icon
}

func (a *app) updateTray() {
	dpi := dpiForWindow(a.primaryTaskbar())
	size := 16 * dpi / 96
	if pGetSystemMetricsForDpi.Find() == nil {
		if s, _, _ := pGetSystemMetricsForDpi.Call(SM_CXSMICON, uintptr(dpi)); s > 0 {
			size = int(s)
		}
	}
	u := a.snapshot()
	icon := makeIcon(render.GroupTrayIconTheme(size, u, a.theme()))

	nid := NOTIFYICONDATAW{HWnd: a.main, UID: 1, UFlags: NIF_MESSAGE | NIF_ICON, UCallbackMessage: msgTray, HIcon: icon}
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	a.setTrayTooltip(&nid, u)
	op := uintptr(NIM_MODIFY)
	if !a.trayAdded {
		op = NIM_ADD
	}
	if r, _, _ := pShellNotifyIconW.Call(op, uintptr(unsafe.Pointer(&nid))); r != 0 {
		a.trayAdded = true
		if op == NIM_ADD {
			a.trayInput = trayInput{}
			nid.UVersion = NOTIFYICON_VERSION_4
			v, _, _ := pShellNotifyIconW.Call(NIM_SETVERSION, uintptr(unsafe.Pointer(&nid)))
			a.trayVersion4 = v != 0
		}
	} else if op == NIM_MODIFY {
		a.trayAdded = false
	}
	if a.trayIcon != 0 {
		pDestroyIcon.Call(a.trayIcon)
	}
	a.trayIcon = icon
}

// Empty text also suppresses legacy-shell tooltips, not only version-4 SHOWTIP.
func (a *app) setTrayTooltip(nid *NOTIFYICONDATAW, readings []usage.Usage) {
	nid.UFlags |= NIF_TIP
	nid.UFlags &^= NIF_SHOWTIP
	for i := range nid.SzTip {
		nid.SzTip[i] = 0
	}
	tip := render.PopupAwareTooltip(readings, a.popupVisible)
	if tip != "" {
		nid.UFlags |= NIF_SHOWTIP
		copyUTF16(nid.SzTip[:], tip)
	}
}

func (a *app) updateTrayTooltip() {
	if !a.trayAdded {
		return
	}
	nid := NOTIFYICONDATAW{HWnd: a.main, UID: 1}
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	a.setTrayTooltip(&nid, a.snapshot())
	pShellNotifyIconW.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&nid)))
}

func (a *app) removeTray() {
	nid := NOTIFYICONDATAW{HWnd: a.main, UID: 1}
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	pShellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&nid)))
	a.trayAdded = false
}

// Context menu

func (a *app) showMenu() {
	var pt POINT
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	a.showMenuAt(pt)
}

func (a *app) showMenuAt(pt POINT) {
	menu, _, _ := pCreatePopupMenu.Call()
	defer pDestroyMenu.Call(menu) // also destroys the submenus attached to it
	add := func(m, flags, id uintptr, text string) {
		pAppendMenuW.Call(m, flags, id, uintptr(unsafe.Pointer(utf16(text))))
	}
	check := func(on bool) uintptr {
		if on {
			return MF_CHECKED
		}
		return 0
	}
	intMenu, _, _ := pCreatePopupMenu.Call()
	for _, m := range intervals {
		add(intMenu, MF_STRING|check(a.cfg.IntervalMinutes == m), uintptr(cmdInterval+m), fmt.Sprintf("Every %d min", m))
	}

	for _, u := range a.snapshot() {
		status := u.Name() + ": updated " + u.UpdatedAt.Format("3:04 PM")
		if u.Err != "" {
			status = u.Name() + ": " + u.Err
		}
		add(menu, MF_STRING|MF_GRAYED, 0, status)
	}
	add(menu, MF_SEPARATOR, 0, "")
	add(menu, MF_STRING, cmdSettings, "Settings...")
	add(menu, MF_STRING, cmdAccessible, "Readable usage (F6)")
	if a.latestRelease.Version != "" {
		add(menu, MF_STRING|MF_GRAYED, 0, a.updateStatus())
	}
	snoozeMenu, _, _ := pCreatePopupMenu.Call()
	add(snoozeMenu, MF_STRING, cmdSnooze30, "30 minutes")
	add(snoozeMenu, MF_STRING, cmdSnooze60, "1 hour")
	add(snoozeMenu, MF_STRING, cmdSnoozeTomorrow, "Until tomorrow at 08:00")
	add(snoozeMenu, MF_STRING, cmdResume, "Resume automatic notices")
	add(menu, MF_POPUP, snoozeMenu, "Snooze automatic notices")
	if a.cfg.SnoozeUntil > time.Now().Unix() {
		add(menu, MF_STRING|MF_GRAYED, 0, "Snoozed until "+time.Unix(a.cfg.SnoozeUntil, 0).Format("Mon 15:04"))
	}
	add(menu, MF_STRING, cmdDetails, "Show details")
	add(menu, MF_STRING, cmdRefresh, "Refresh now")
	providerMenu, _, _ := pCreatePopupMenu.Call()
	for i, mode := range providerModes {
		add(providerMenu, MF_STRING|check(a.cfg.Providers == mode.key), uintptr(cmdProvider+i), mode.label)
	}
	add(menu, MF_POPUP, providerMenu, "Usage providers")
	add(menu, MF_SEPARATOR, 0, "")
	add(menu, MF_STRING|check(a.cfg.MilestonePopups), cmdMilestones, "Automatic usage milestones")
	add(menu, MF_STRING|check(a.cfg.AlwaysShowPopup), cmdAlwaysShow, "Always show popup (optional)")
	delayMenu, _, _ := pCreatePopupMenu.Call()
	for _, seconds := range popupDelays {
		add(delayMenu, MF_STRING|check(a.cfg.PopupSeconds == seconds), uintptr(cmdPopupDelay+seconds), fmt.Sprintf("%d seconds", seconds))
	}
	add(menu, MF_POPUP, delayMenu, "Close popup after")
	add(menu, MF_POPUP, intMenu, "Refresh interval")
	add(menu, MF_STRING|check(a.cfg.Alerts), cmdAlerts, "Windows alerts (80% / 95% / reset)")
	add(menu, MF_STRING|check(startupEnabled()), cmdStartup, "Start with Windows")
	add(menu, MF_SEPARATOR, 0, "")
	add(menu, MF_STRING, cmdAbout, "About BurnBar...")
	add(menu, MF_STRING, cmdRepository, "Open GitHub repository")
	add(menu, MF_SEPARATOR, 0, "")
	add(menu, MF_STRING, cmdExit, "Exit")

	pSetForegroundWindow.Call(a.main)
	cmd, _, _ := pTrackPopupMenu.Call(menu, TPM_RETURNCMD|TPM_RIGHTBUTTON|TPM_BOTTOMALIGN,
		uintptr(pt.X), uintptr(pt.Y), 0, a.main, 0)
	pPostMessageW.Call(a.main, WM_NULL, 0, 0)

	switch {
	case cmd == cmdSettings:
		a.showSettings()
		return
	case cmd == cmdAccessible:
		a.showAccessible()
		return
	case cmd == cmdSnooze30:
		a.snooze(30)
	case cmd == cmdSnooze60:
		a.snooze(60)
	case cmd == cmdSnoozeTomorrow:
		a.snooze(-1)
	case cmd == cmdResume:
		a.snooze(0)
	case cmd == cmdDetails:
		a.popupHiddenAt = time.Time{}
		a.popupTB = a.primaryTaskbar()
		a.popupExpanded = true
		a.showPopup(true)
	case cmd == cmdMilestones:
		a.cfg.MilestonePopups = !a.cfg.MilestonePopups
		if !a.cfg.MilestonePopups && a.popupLifetime.Automatic() {
			a.hidePopup()
		}
	case cmd >= cmdPopupDelay+1 && cmd <= cmdPopupDelay+300:
		a.cfg.PopupSeconds = int(cmd - cmdPopupDelay)
		if a.popupVisible && (!a.cfg.AlwaysShowPopup || a.popupExpanded) {
			if a.popupLifetime.Automatic() {
				a.popupLifetime.Clear()
				a.popupLifetime.StartAutomatic(time.Now(), a.popupDelay())
				a.oneShot(timerPopup, a.popupDelay())
			} else {
				a.manualPopup()
			}
		}
	case cmd == cmdAlwaysShow:
		a.clearPopup()
		a.cfg.AlwaysShowPopup = !a.cfg.AlwaysShowPopup
		a.popupExpanded = false
		if !a.cfg.AlwaysShowPopup {
			a.hidePopup()
		}
	case cmd == cmdRefresh:
		a.refresh(true)
	case cmd == cmdAlerts:
		a.cfg.Alerts = !a.cfg.Alerts
	case cmd == cmdStartup:
		on := !startupEnabled()
		if err := setStartup(on); err != nil {
			a.showMessage("Start with Windows could not be updated", err.Error())
			return
		}
		a.cfg.StartWithWindows = &on
	case cmd == cmdAbout:
		a.showAbout()
		return
	case cmd == cmdRepository:
		a.openRepository()
		return
	case cmd >= cmdProvider && cmd < cmdProvider+uintptr(len(providerModes)):
		a.cfg.Providers = providerModes[cmd-cmdProvider].key
		a.refresh(false)
		a.scheduleResetRefresh()
	case cmd > cmdInterval && cmd <= cmdInterval+60:
		a.cfg.IntervalMinutes = int(cmd - cmdInterval)
		a.setRefreshTimer()
	case cmd == cmdExit:
		pPostQuitMessage.Call(0)
		return
	default:
		return
	}
	a.cfg.Save()
	a.updateTray()
	if a.popupVisible {
		a.renderPopup()
	}
	a.syncPopup()
}

// Window procedures

func mainProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	a := theApp
	switch {
	case msg == WM_TIMER:
		switch wparam {
		case timerPopup:
			a.expirePopup()
		case timerPopupLayout:
			if a.accessible != nil && !time.Now().Before(a.accessibleDeadline) {
				closeNative(&a.accessible)
			}
			a.checkUpdates(false)
			a.updateAccessible()
			a.syncPopup()
		case timerRefresh:
			a.refresh(false)
		case timerReset:
			pKillTimer.Call(a.main, timerReset)
			a.refresh(false)
		case timerActive:
			a.refreshActivity()
		}
		return 0
	case msg == wmCommand:
		if wparam&0xffff == cmdSettings {
			a.showSettings()
		}
		if wparam&0xffff == cmdAccessible {
			a.showAccessible()
		}
		return 0
	case msg == wmHotkey:
		if wparam == hotkeyID {
			a.showAccessible()
		}
		return 0
	case msg == msgUpdate:
		a.onUpdate()
		return 0
	case msg == msgUsage:
		a.onUsage(providerFromID(wparam))
		return 0
	case msg == msgActivity:
		a.onActivity(providerFromID(wparam))
		return 0
	case msg == msgShellEvent:
		a.syncPopup()
		return 0
	case msg == msgTray:
		a.handleTray(wparam, lparam)
		return 0
	case msg == msgTrayAction:
		pt := unpackTrayPoint(lparam)
		switch trayAction(wparam) {
		case trayMenu:
			a.showMenuAt(pt)
		case trayDetails:
			a.togglePopup()
		}
		return 0
	case msg == msgMenu:
		a.showMenu()
		return 0
	case a != nil && a.taskbarCreated != 0 && uint32(msg) == a.taskbarCreated:
		// Explorer restarted: the tray icon is gone and the taskbars are new windows.
		a.trayAdded = false
		a.popupTB = a.primaryTaskbar()
		a.updateTray()
		a.syncPopup()
		return 0
	case msg == WM_DESTROY:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

func popupProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	a := theApp
	switch msg {
	case WM_CLOSE:
		a.dismissPopup()
		return 0
	case WM_ACTIVATE:
		// Close when the user clicks elsewhere. If Windows refused to give the popup focus,
		// the configured timeout still closes it.
		if wparam&0xFFFF != WA_INACTIVE {
			a.popupActive = true
		} else if a.popupVisible && a.popupActive {
			a.dismissPopup()
		}
		return 0
	case WM_KEYDOWN:
		switch wparam {
		case VK_ESCAPE:
			a.dismissPopup()
		case 0x0d, 0x20:
			a.manualPopup()
			a.popupExpanded = true
			a.showPopup(true)
		case 'R':
			a.manualPopup()
			a.refresh(true)
		case 'S':
			a.showSettings()
		case 0x75, 0x09:
			a.showAccessible() // F6 / Tab: native readable view
		case 0x5d:
			pPostMessageW.Call(a.main, msgMenu, 0, 0)
		}
		return 0
	case WM_LBUTTONUP:
		a.manualPopup()
		if !a.popupExpanded {
			a.popupExpanded = true
			a.renderPopup()
		} else {
			a.refresh(true)
		}
		return 0
	case WM_RBUTTONUP:
		pPostMessageW.Call(a.main, msgMenu, 0, 0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

// winEventProc only posts work: changing z-order or querying the shell from a
// hook can re-enter Explorer while it is handling focus or a tray drag.
func winEventProc(hook, event, hwnd, idObject, idChild, thread, ts uintptr) uintptr {
	a := theApp
	if a == nil {
		return 0
	}
	if event == EVENT_SYS_FOREGND {
		pPostMessageW.Call(a.main, msgShellEvent, event, 0)
	}
	return 0
}
