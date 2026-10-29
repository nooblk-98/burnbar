package app

import (
	"fmt"
	"image"
	"os"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/nooblk-98/burnbar/internal/config"
	"github.com/nooblk-98/burnbar/internal/history"
	"github.com/nooblk-98/burnbar/internal/layout"
	"github.com/nooblk-98/burnbar/internal/render"
	"github.com/nooblk-98/burnbar/internal/usage"

	"golang.org/x/sys/windows/registry"
)

const (
	msgUsage    = WM_APP + 1 // background fetch finished
	msgTray     = WM_APP + 2 // notification-area icon callback
	msgMenu     = WM_APP + 3 // overlay or popup asks for the context menu
	msgActivity = WM_APP + 4 // a provider wrote to its session logs

	timerLayout   = 1 // 1s: z-order, fullscreen check, cached placement
	timerRefresh  = 2 // regular usage refresh
	timerRelayout = 3 // shortly after windows open or close
	timerReset    = 4 // just after the next limit resets
	timerActive   = 5 // deferred refresh while Claude Code is busy
	timerRotate   = 6 // switch the compact meter to the next provider
	timerFlip     = 7 // animate only while a flip is in progress

	cmdDetails   = 1
	cmdRefresh   = 2
	cmdOverlay   = 3
	cmdMonitors  = 4
	cmdAlerts    = 5
	cmdStartup   = 6
	cmdExit      = 9
	cmdPosition  = 100 // + index into positions
	cmdInterval  = 200 // + minutes
	cmdProvider  = 300 // + index into providerModes
	slotTTL      = 5 * time.Second
	activeMinGap = 60 * time.Second

	// After a window opens or closes, the taskbar slides its buttons and may collapse the
	// weather widget for a second or more. Re-read it until it holds still.
	relayoutEvery     = 250 * time.Millisecond
	relayoutMaxPasses = 20 // 5s
	relayoutSteady    = 3  // unchanged reads in a row before the layout counts as settled

	runKey         = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue       = "BurnBar"
	legacyRunValue = "ClaudeUsageMeter" // pre-rename builds
)

var (
	positions     = []struct{ key, label string }{{"auto", "Auto (find free space)"}, {"left", "Left side"}, {"right", "Next to tray icons"}}
	intervals     = []int{1, 3, 5, 10}
	providerModes = []struct{ key, label string }{{"both", "Claude + Codex (rotate every 8 sec)"}, {"claude", "Claude only"}, {"codex", "Codex only"}}
)

type placement struct {
	x, y, w, h int
	scale      float64
	light      bool
	visible    bool
	floating   bool
}

// overlay is one meter window, attached to one taskbar.
type overlay struct {
	hwnd  uintptr
	tb    uintptr
	last  placement
	hover bool

	slotX            int
	slotW            int
	slotActualW      int
	slotOK           bool
	slotAt           time.Time
	slotLayout       string // taskbar spans the slot was chosen from
	flipAt           time.Time
	flipFrom, flipTo *image.RGBA
	flipScale        float64
	flipWidth        int
	flipLight        bool
}

type app struct {
	demo bool
	cfg  config.Config
	hist map[usage.Provider]*history.History

	hinst uintptr
	main  uintptr
	popup uintptr

	mu       sync.Mutex
	readings map[usage.Provider]usage.Usage
	fetches  map[usage.Provider]*fetchState

	// main-thread state
	lastFetch     time.Time
	activePending bool

	overlays       map[uintptr]*overlay // by taskbar window
	slotsDirty     bool
	layoutMoved    bool // a taskbar's spans changed since the previous read
	relayoutPasses int
	steadyPasses   int
	activeProvider int
	flipStarted    time.Time
	flipPrevious   int

	popupVisible  bool
	popupActive   bool // became the foreground window at least once since shown
	popupHiddenAt time.Time
	popupTB       uintptr
	popupCenterX  int
	popupMinute   int

	trayIcon       uintptr
	trayAdded      bool
	taskbarCreated uint32

	uia *uiaClient
}

type fetchState struct {
	fetching      atomic.Bool
	cooldownUntil time.Time
	backoff       time.Duration
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
	mainProcCB    = windowsCallback(mainProc)
	overlayProcCB = windowsCallback(overlayProc)
	popupProcCB   = windowsCallback(popupProc)
	winEventCB    = windowsCallback7(winEventProc)
)

func (a *app) run() error {
	pSetProcessDpiAwarenessContext.Call(DPI_PER_MONITOR_V2)
	a.hinst, _, _ = pGetModuleHandleW.Call(0)
	cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	icon, _, _ := pLoadImageW.Call(a.hinst, uintptr(unsafe.Pointer(utf16("APP"))), IMAGE_ICON, 0, 0, LR_DEFAULTSIZE)
	a.overlays = map[uintptr]*overlay{}

	for _, c := range []struct {
		name string
		proc uintptr
	}{{"BurnBarMain", mainProcCB}, {"BurnBarOverlay", overlayProcCB}, {"BurnBarPopup", popupProcCB}} {
		wc := WNDCLASSEXW{LpfnWndProc: c.proc, HInstance: a.hinst, HCursor: cursor, HIcon: icon, LpszClassName: utf16(c.name)}
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

	migrateStartup()
	a.uia = newUIA()
	r, _, _ := pRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(utf16("TaskbarCreated"))))
	a.taskbarCreated = uint32(r)
	pSetWinEventHook.Call(EVENT_SYS_FOREGND, EVENT_SYS_FOREGND, 0, winEventCB, 0, 0, WINEVENT_OUTOFCTX)
	// Windows appearing or disappearing usually means taskbar buttons moved.
	pSetWinEventHook.Call(EVENT_OBJECT_DESTROY, EVENT_OBJECT_HIDE, 0, winEventCB, 0, 0, WINEVENT_OUTOFCTX|WINEVENT_SKIPOWNPROCESS)
	if !a.demo {
		for _, p := range usage.Providers("both") {
			watchActivity(p, func() { pPostMessageW.Call(a.main, msgActivity, providerID(p), 0) })
		}
	}

	a.place(true)
	a.updateTray()
	a.refresh(true)
	pSetTimer.Call(a.main, timerLayout, 1000, 0)
	a.setRefreshTimer()
	a.resetRotation()

	var m MSG
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
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
	if !manual && time.Now().Before(state.cooldownUntil) {
		return
	}
	if a.demo {
		a.mu.Lock()
		a.readings[p] = usage.DemoProvider(p)
		a.mu.Unlock()
		pPostMessageW.Call(a.main, msgUsage, providerID(p), 0)
		return
	}
	if !state.fetching.CompareAndSwap(false, true) {
		return
	}
	a.lastFetch = time.Now()
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
		}
		a.checkAlerts(u)
	}
	a.scheduleResetRefresh()
	a.place(true)
	a.updateTray()
	if a.popupVisible {
		a.renderPopup()
	}
}

// onActivity refreshes soon after Claude Code is used, but no more than once a minute.
func (a *app) onActivity(p usage.Provider) {
	if !a.enabled(p) {
		return
	}
	if a.activePending {
		return
	}
	if wait := activeMinGap - time.Since(a.lastFetch); wait > 0 {
		a.activePending = true
		a.oneShot(timerActive, wait)
		return
	}
	a.refresh(false)
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
		if seen && prev.ResetsAt != cycle && prev.Level > 0 && w.Percent < prev.Percent && a.cfg.Alerts {
			a.notify(u.Name()+" "+w.Label+" limit has reset", fmt.Sprintf("You're back to %.0f%% used.", w.Percent), false)
		}
		level := 0
		if w.Percent >= 95 {
			level = 95
		} else if w.Percent >= 80 {
			level = 80
		}
		if level > cur.Level {
			if a.cfg.Alerts {
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

// Taskbar overlays

func systemUsesLightTheme() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("SystemUsesLightTheme")
	return err == nil && v == 1
}

func (a *app) newOverlay(tb uintptr) *overlay {
	hwnd, _, _ := pCreateWindowExW.Call(WS_EX_LAYERED|WS_EX_TOOLWINDOW|WS_EX_TOPMOST|WS_EX_NOACTIVATE,
		uintptr(unsafe.Pointer(utf16("BurnBarOverlay"))), uintptr(unsafe.Pointer(utf16("BurnBar"))),
		WS_POPUP, 0, 0, 0, 0, 0, 0, a.hinst, 0)
	return &overlay{hwnd: hwnd, tb: tb}
}

func (a *app) overlayByHwnd(hwnd uintptr) *overlay {
	for _, ov := range a.overlays {
		if ov.hwnd == hwnd {
			return ov
		}
	}
	return nil
}

// slot returns the overlay's x position, re-reading the taskbar layout every few seconds
// or right after windows open and close.
func (a *app) slot(ov *overlay, tb taskbar, w int, scale float64) (int, int, bool) {
	if !a.slotsDirty && w == ov.slotW && time.Since(ov.slotAt) < slotTTL {
		return ov.slotX, ov.slotActualW, ov.slotOK
	}
	margin := int(6 * scale)
	items, ok := a.uia.items(tb.hwnd)
	occupied, trayLeft, hasTray := layout.AnalyzeTaskbar(items, tb.rect)
	if !ok || (!hasTray && len(occupied) == 0) {
		// Explorer can temporarily stop answering automation calls while Search
		// or a flyout opens. Keep the last safe slot briefly and retry next tick.
		a.layoutMoved = true
		if ov.slotOK && ov.slotW == w && time.Since(ov.slotAt) < slotTTL+2*time.Second && ov.slotX >= int(tb.rect.Left)+margin && ov.slotX+w <= int(tb.rect.Right)-margin {
			return ov.slotX, ov.slotActualW, true
		}
		return ov.slotX, w, false
	}
	if spans := fmt.Sprint(occupied, trayLeft); spans != ov.slotLayout {
		ov.slotLayout = spans
		a.layoutMoved = true
	}
	if !hasTray {
		trayLeft = int(tb.rect.Right) - int(300*scale)
		if !tb.primary {
			trayLeft = int(tb.rect.Right) - int(140*scale) // clock only
		}
	}
	x, width, fits := layout.PickSizedSlot(occupied, int(tb.rect.Left), trayLeft, w, render.MinimumWidgetWidth(scale), margin, a.cfg.Position)
	if !fits {
		width = w
		x = ov.slotX
		if ov.slotAt.IsZero() {
			x = trayLeft - margin - w
		}
		x = min(max(x, int(tb.rect.Left)+margin), int(tb.rect.Right)-margin-w)
	}
	ov.slotX, ov.slotW, ov.slotAt, ov.slotOK = x, w, time.Now(), fits
	ov.slotActualW = width
	return x, width, fits
}

func (a *app) place(force bool) {
	light := systemUsesLightTheme()
	hideAll := !a.cfg.ShowOverlay || fullscreenAppActive()
	seen := map[uintptr]bool{}
	for _, tb := range findTaskbars(a.cfg.AllMonitors) {
		seen[tb.hwnd] = true
		ov := a.overlays[tb.hwnd]
		if ov == nil {
			ov = a.newOverlay(tb.hwnd)
			a.overlays[tb.hwnd] = ov
		}
		p := placement{}
		if v, _, _ := pIsWindowVisible.Call(tb.hwnd); v != 0 && !hideAll {
			scale := float64(dpiForWindow(tb.hwnd)) / 96
			w, h := render.GroupWidgetSize(scale, len(usage.Providers(a.cfg.Providers)))
			x, actualW, fits := a.slot(ov, tb, w, scale)
			w = actualW
			y := int(tb.rect.Top) + (int(tb.rect.Bottom-tb.rect.Top)-h)/2
			if !fits {
				y = int(tb.rect.Top) - h - int(4*scale)
			}
			p = placement{x: x, y: y, w: w, h: h, scale: scale, light: light, visible: true, floating: !fits}
		}
		a.apply(ov, p, force)
	}
	a.slotsDirty = false
	for tbh, ov := range a.overlays {
		if !seen[tbh] {
			pDestroyWindow.Call(ov.hwnd)
			delete(a.overlays, tbh)
		}
	}
}

func (a *app) apply(ov *overlay, p placement, force bool) {
	if !p.visible {
		if ov.last.visible {
			pShowWindow.Call(ov.hwnd, SW_HIDE)
		}
		ov.last = p
		return
	}
	if force || p != ov.last {
		img := a.widget(ov, p)
		if p.floating {
			img = render.FloatingWidget(img, render.NewTheme(p.light), p.scale)
		}
		a.paint(ov.hwnd, img, p.x, p.y)
	}
	if !ov.last.visible {
		pShowWindow.Call(ov.hwnd, SW_SHOWNOACTIVATE)
	}
	pSetWindowPos.Call(ov.hwnd, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE|SWP_SHOWWINDOW)
	ov.last = p
}

func (a *app) raise() {
	for _, ov := range a.overlays {
		if ov.last.visible {
			pSetWindowPos.Call(ov.hwnd, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE|SWP_SHOWWINDOW)
		}
	}
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

// togglePopup opens the details panel above the given taskbar, centred on centerX.
func (a *app) togglePopup(tb uintptr, centerX int) {
	if a.popupVisible {
		a.hidePopup()
		return
	}
	// The click that deactivated (and so closed) the popup should not immediately reopen it.
	if time.Since(a.popupHiddenAt) < 300*time.Millisecond {
		return
	}
	a.popupTB, a.popupCenterX = tb, centerX
	a.renderPopup()
	a.popupVisible, a.popupActive = true, false
	pShowWindow.Call(a.popup, SW_SHOW)
	pSetForegroundWindow.Call(a.popup)
}

func (a *app) hidePopup() {
	pShowWindow.Call(a.popup, SW_HIDE)
	a.popupVisible = false
	a.popupHiddenAt = time.Now()
}

func (a *app) renderPopup() {
	scale := float64(dpiForWindow(a.popupTB)) / 96
	var panels []render.Panel
	for _, u := range a.snapshot() {
		panels = append(panels, render.Panel{Usage: u, History: a.hist[u.Provider].Since(time.Now().Add(-7 * 24 * time.Hour))})
	}
	work := workArea(a.popupTB)
	for _, ov := range a.overlays {
		if ov.tb == a.popupTB && ov.last.visible && ov.last.floating {
			work.Bottom = min(work.Bottom, int32(ov.last.y))
		}
	}
	// Two columns on roomy screens; a compact stacked summary on small displays.
	img := render.GroupPopup(scale, panels, render.NewTheme(systemUsesLightTheme()), int(work.Right-work.Left)-24, int(work.Bottom-work.Top)-24)
	w, h := img.Rect.Dx(), img.Rect.Dy()
	gapPx := int(12 * scale)
	x := min(max(a.popupCenterX-w/2, int(work.Left)+gapPx), int(work.Right)-gapPx-w)
	y := int(work.Bottom) - gapPx - h
	a.paint(a.popup, img, x, y)
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
	icon := makeIcon(render.GroupTrayIcon(size, u, systemUsesLightTheme()))

	nid := NOTIFYICONDATAW{HWnd: a.main, UID: 1, UFlags: NIF_MESSAGE | NIF_ICON | NIF_TIP, UCallbackMessage: msgTray, HIcon: icon}
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	copyUTF16(nid.SzTip[:], render.GroupTooltip(u))
	op := uintptr(NIM_MODIFY)
	if !a.trayAdded {
		op = NIM_ADD
	}
	if r, _, _ := pShellNotifyIconW.Call(op, uintptr(unsafe.Pointer(&nid))); r != 0 {
		a.trayAdded = true
	} else if op == NIM_MODIFY {
		a.trayAdded = false
	}
	if a.trayIcon != 0 {
		pDestroyIcon.Call(a.trayIcon)
	}
	a.trayIcon = icon
}

func (a *app) removeTray() {
	nid := NOTIFYICONDATAW{HWnd: a.main, UID: 1}
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	pShellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&nid)))
	a.trayAdded = false
}

// Start with Windows

func startupEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runValue)
	return err == nil
}

func setStartup(on bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	if !on {
		k.DeleteValue(runValue)
		return
	}
	if exe, err := os.Executable(); err == nil {
		k.SetStringValue(runValue, `"`+exe+`"`)
	}
}

// migrateStartup replaces a start-with-Windows entry left by a pre-rename build with one for this exe.
func migrateStartup() {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	if _, _, err := k.GetStringValue(legacyRunValue); err == nil {
		k.DeleteValue(legacyRunValue)
		setStartup(true)
	}
}

// Context menu

func (a *app) showMenu() {
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
	posMenu, _, _ := pCreatePopupMenu.Call()
	for i, p := range positions {
		add(posMenu, MF_STRING|check(a.cfg.Position == p.key), uintptr(cmdPosition+i), p.label)
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
	add(menu, MF_STRING, cmdDetails, "Show details")
	add(menu, MF_STRING, cmdRefresh, "Refresh now")
	providerMenu, _, _ := pCreatePopupMenu.Call()
	for i, mode := range providerModes {
		add(providerMenu, MF_STRING|check(a.cfg.Providers == mode.key), uintptr(cmdProvider+i), mode.label)
	}
	add(menu, MF_POPUP, providerMenu, "Usage providers")
	add(menu, MF_SEPARATOR, 0, "")
	add(menu, MF_STRING|check(a.cfg.ShowOverlay), cmdOverlay, "Show on taskbar")
	add(menu, MF_STRING|check(a.cfg.AllMonitors), cmdMonitors, "Show on all monitors")
	add(menu, MF_POPUP, posMenu, "Position")
	add(menu, MF_POPUP, intMenu, "Refresh interval")
	add(menu, MF_STRING|check(a.cfg.Alerts), cmdAlerts, "Usage alerts (80% / 95% / reset)")
	add(menu, MF_STRING|check(startupEnabled()), cmdStartup, "Start with Windows")
	add(menu, MF_SEPARATOR, 0, "")
	add(menu, MF_STRING, cmdExit, "Exit")

	var pt POINT
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(a.main)
	cmd, _, _ := pTrackPopupMenu.Call(menu, TPM_RETURNCMD|TPM_RIGHTBUTTON|TPM_BOTTOMALIGN,
		uintptr(pt.X), uintptr(pt.Y), 0, a.main, 0)
	pPostMessageW.Call(a.main, WM_NULL, 0, 0)

	switch {
	case cmd == cmdDetails:
		a.popupHiddenAt = time.Time{}
		a.togglePopup(a.primaryTaskbar(), int(pt.X))
	case cmd == cmdRefresh:
		a.refresh(true)
	case cmd == cmdOverlay:
		a.cfg.ShowOverlay = !a.cfg.ShowOverlay
	case cmd == cmdMonitors:
		a.cfg.AllMonitors = !a.cfg.AllMonitors
	case cmd == cmdAlerts:
		a.cfg.Alerts = !a.cfg.Alerts
	case cmd == cmdStartup:
		setStartup(!startupEnabled())
	case cmd >= cmdProvider && cmd < cmdProvider+uintptr(len(providerModes)):
		a.cfg.Providers = providerModes[cmd-cmdProvider].key
		a.resetRotation()
		a.refresh(false)
		a.scheduleResetRefresh()
	case cmd >= cmdPosition && cmd < cmdPosition+uintptr(len(positions)):
		a.cfg.Position = positions[cmd-cmdPosition].key
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
	a.slotsDirty = true
	a.place(true)
	a.updateTray()
	if a.popupVisible {
		a.renderPopup()
	}
}

// Window procedures

func mainProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	a := theApp
	switch {
	case msg == WM_TIMER:
		switch wparam {
		case timerLayout:
			a.place(false)
			if a.popupVisible && time.Now().Minute() != a.popupMinute {
				a.renderPopup() // keep "resets in" countdowns current
			}
		case timerRefresh:
			a.refresh(false)
		case timerRotate:
			a.rotateProvider()
		case timerFlip:
			a.animateFlip()
		case timerRelayout:
			// Follow the taskbar's animation: a slot read mid-slide can end up under Start.
			a.slotsDirty = true
			a.layoutMoved = false
			a.place(false)
			if a.layoutMoved {
				a.steadyPasses = 0
			} else {
				a.steadyPasses++
			}
			if a.relayoutPasses--; a.relayoutPasses > 0 && a.steadyPasses < relayoutSteady {
				a.oneShot(timerRelayout, relayoutEvery)
			} else {
				a.relayoutPasses = 0
				pKillTimer.Call(a.main, timerRelayout)
			}
		case timerReset:
			pKillTimer.Call(a.main, timerReset)
			a.refresh(false)
		case timerActive:
			pKillTimer.Call(a.main, timerActive)
			a.activePending = false
			a.refresh(false)
		}
		return 0
	case msg == msgUsage:
		a.onUsage(providerFromID(wparam))
		return 0
	case msg == msgActivity:
		a.onActivity(providerFromID(wparam))
		return 0
	case msg == msgTray:
		switch lparam & 0xFFFF {
		case WM_RBUTTONUP:
			a.showMenu()
		case WM_LBUTTONUP:
			var pt POINT
			pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
			a.togglePopup(a.primaryTaskbar(), int(pt.X))
		}
		return 0
	case msg == msgMenu:
		a.showMenu()
		return 0
	case a != nil && a.taskbarCreated != 0 && uint32(msg) == a.taskbarCreated:
		// Explorer restarted: the tray icon is gone and the taskbars are new windows.
		a.trayAdded = false
		a.updateTray()
		a.slotsDirty = true
		a.place(true)
		return 0
	case msg == WM_DESTROY:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

func overlayProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	a := theApp
	ov := a.overlayByHwnd(hwnd)
	if ov == nil {
		r, _, _ := pDefWindowProcW.Call(hwnd, msg, wparam, lparam)
		return r
	}
	switch msg {
	case WM_MOUSEMOVE:
		if !ov.hover {
			ov.hover = true
			tme := TRACKMOUSEEVENT{DwFlags: TME_LEAVE, HwndTrack: hwnd}
			tme.CbSize = uint32(unsafe.Sizeof(tme))
			pTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
			a.place(true)
		}
		return 0
	case WM_MOUSELEAVE:
		ov.hover = false
		a.place(true)
		return 0
	case WM_LBUTTONUP:
		a.togglePopup(ov.tb, ov.last.x+ov.last.w/2)
		return 0
	case WM_RBUTTONUP:
		pPostMessageW.Call(a.main, msgMenu, 0, 0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

func popupProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	a := theApp
	switch msg {
	case WM_ACTIVATE:
		// Close when the user clicks elsewhere. If Windows refused to give the popup focus,
		// it stays open until the widget or tray icon is clicked again.
		if wparam&0xFFFF != WA_INACTIVE {
			a.popupActive = true
		} else if a.popupVisible && a.popupActive {
			a.hidePopup()
		}
		return 0
	case WM_KEYDOWN:
		if wparam == VK_ESCAPE {
			a.hidePopup()
		}
		return 0
	case WM_LBUTTONUP:
		a.refresh(true)
		return 0
	case WM_RBUTTONUP:
		pPostMessageW.Call(a.main, msgMenu, 0, 0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

// winEventProc re-raises the meter when focus changes (clicking the taskbar brings it back above
// other topmost windows) and schedules a relayout when top-level windows appear or disappear.
func winEventProc(hook, event, hwnd, idObject, idChild, thread, ts uintptr) uintptr {
	a := theApp
	if a == nil {
		return 0
	}
	if event == EVENT_SYS_FOREGND {
		a.raise()
		return 0
	}
	if int32(idObject) == OBJID_WINDOW && idChild == 0 {
		a.steadyPasses = 0
		if a.relayoutPasses == 0 {
			a.relayoutPasses = relayoutMaxPasses
			a.oneShot(timerRelayout, relayoutEvery)
		}
	}
	return 0
}
