//go:build windows

package app

import (
	"fmt"
	"math"
	"strings"
	"time"
	utf16codec "unicode/utf16"
	"unsafe"
)

var (
	pSendMessageW          = user32.NewProc("SendMessageW")
	pSetWindowTextW        = user32.NewProc("SetWindowTextW")
	pGetWindowTextW        = user32.NewProc("GetWindowTextW")
	pDestroyWindow         = user32.NewProc("DestroyWindow")
	pSetFocus              = user32.NewProc("SetFocus")
	pIsDialogMessageW      = user32.NewProc("IsDialogMessageW")
	pSetWindowPos          = user32.NewProc("SetWindowPos")
	pAdjustWindowRectEx    = user32.NewProc("AdjustWindowRectEx")
	pCreateFontW           = gdi32.NewProc("CreateFontW")
	pRegisterHotKey        = user32.NewProc("RegisterHotKey")
	pUnregisterHotKey      = user32.NewProc("UnregisterHotKey")
	pSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")
	pGetSysColor           = user32.NewProc("GetSysColor")
	pOpenClipboard         = user32.NewProc("OpenClipboard")
	pCloseClipboard        = user32.NewProc("CloseClipboard")
	pEmptyClipboard        = user32.NewProc("EmptyClipboard")
	pSetClipboardData      = user32.NewProc("SetClipboardData")
	pGlobalAlloc           = kernel32.NewProc("GlobalAlloc")
	pGlobalLock            = kernel32.NewProc("GlobalLock")
	pGlobalUnlock          = kernel32.NewProc("GlobalUnlock")
	pGlobalFree            = kernel32.NewProc("GlobalFree")
	controlsProcCB         = windowsCallback(controlsProc)
)

const (
	wmCommand    = 0x111
	wmSetFont    = 0x30
	wmDPIChanged = 0x02e0
	wmHotkey     = 0x0312
	wsChild      = 0x40000000
	wsVisible    = 0x10000000
	wsTabStop    = 0x10000
	wsVScroll    = 0x200000
	windowStyle  = 0x00C00000 | 0x00080000 | 0x00020000 // caption, system menu, minimize
	bmSetCheck   = 0xf1
	bmGetCheck   = 0xf0
	cbAddString  = 0x143
	cbSetCurSel  = 0x14e
	cbGetCurSel  = 0x147
	hotkeyID     = 1
)

type nativeControl struct {
	hwnd       uintptr
	x, y, w, h int
	page       int
}
type nativeWindow struct {
	hwnd, font    uintptr
	scale         float64
	controls      map[int]nativeControl
	page          int
	width, height int
}

func setText(hwnd uintptr, text string) {
	pSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(utf16(text))))
}
func getText(hwnd uintptr) string {
	text := make([]uint16, 4096)
	n, _, _ := pGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)))
	return stringUTF16(text[:n])
}
func stringUTF16(text []uint16) string { // use the same UTF-16 conversion as Win32
	return strings.TrimRight(string(utf16codec.Decode(text)), "\x00")
}
func (w *nativeWindow) add(id int, class, text string, style uintptr, x, y, width, height, page int) uintptr {
	hwnd, _, _ := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(utf16(class))), uintptr(unsafe.Pointer(utf16(text))), wsChild|wsVisible|style, 0, 0, 0, 0, w.hwnd, uintptr(id), theApp.hinst, 0)
	w.controls[id] = nativeControl{hwnd, x, y, width, height, page}
	return hwnd
}
func (w *nativeWindow) label(id int, text string, x, y, width, page int) {
	w.add(id, "STATIC", text, 0, x, y, width, 20, page)
}
func (w *nativeWindow) edit(id int, text string, x, y, width, page int) {
	w.add(id, "EDIT", text, wsTabStop|0x00800000|0x80, x, y, width, 25, page)
}
func (w *nativeWindow) check(id int, text string, on bool, x, y, width, page int) {
	h := w.add(id, "BUTTON", text, wsTabStop|3, x, y, width, 25, page)
	if on {
		pSendMessageW.Call(h, bmSetCheck, 1, 0)
	}
}
func (w *nativeWindow) button(id int, text string, x, y, width, page int) {
	w.add(id, "BUTTON", text, wsTabStop, x, y, width, 28, page)
}
func (w *nativeWindow) combo(id int, values []string, selected int, x, y, width, page int) {
	h := w.add(id, "COMBOBOX", "", wsTabStop|wsVScroll|3, x, y, width, 180, page)
	for _, s := range values {
		pSendMessageW.Call(h, cbAddString, 0, uintptr(unsafe.Pointer(utf16(s))))
	}
	pSendMessageW.Call(h, cbSetCurSel, uintptr(selected), 0)
}
func (w *nativeWindow) text(id int) string { return getText(w.controls[id].hwnd) }
func (w *nativeWindow) checked(id int) bool {
	r, _, _ := pSendMessageW.Call(w.controls[id].hwnd, bmGetCheck, 0, 0)
	return r == 1
}
func (w *nativeWindow) selected(id int) int {
	r, _, _ := pSendMessageW.Call(w.controls[id].hwnd, cbGetCurSel, 0, 0)
	return int(int32(r))
}
func (w *nativeWindow) set(id int, text string) { setText(w.controls[id].hwnd, text) }
func (w *nativeWindow) selectPage(page int) {
	w.page = page
	for _, c := range w.controls {
		mode := uintptr(SW_HIDE)
		if c.page < 0 || c.page == page {
			mode = SW_SHOWNOACTIVATE
		}
		pShowWindow.Call(c.hwnd, mode)
	}
}
func (w *nativeWindow) layout(dpi int) {
	area := workArea(w.hwnd)
	scale := min(float64(dpi)/96, float64(area.Right-area.Left-24)/float64(w.width+20), float64(area.Bottom-area.Top-24)/float64(w.height+48))
	w.scale = max(0.5, scale)
	old := w.font
	size := int32(-math.Round(14 * w.scale))
	w.font, _, _ = pCreateFontW.Call(uintptr(size), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(utf16("Segoe UI"))))
	for _, c := range w.controls {
		pSendMessageW.Call(c.hwnd, wmSetFont, w.font, 1)
		pSetWindowPos.Call(c.hwnd, 0, uintptr(int(float64(c.x)*w.scale)), uintptr(int(float64(c.y)*w.scale)), uintptr(int(float64(c.w)*w.scale)), uintptr(int(float64(c.h)*w.scale)), 0x14)
	}
	if old != 0 {
		pDeleteObject.Call(old)
	}
	rect := RECT{Right: int32(float64(w.width) * w.scale), Bottom: int32(float64(w.height) * w.scale)}
	adjustDPI := user32.NewProc("AdjustWindowRectExForDpi")
	if adjustDPI.Find() == nil {
		adjustDPI.Call(uintptr(unsafe.Pointer(&rect)), windowStyle, 0, 0, uintptr(dpi))
	} else {
		pAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&rect)), windowStyle, 0, 0)
	}
	width, height := int(rect.Right-rect.Left), int(rect.Bottom-rect.Top)
	pSetWindowPos.Call(w.hwnd, 0, uintptr(int(area.Left)+(int(area.Right-area.Left)-width)/2), uintptr(int(area.Top)+(int(area.Bottom-area.Top)-height)/2), uintptr(width), uintptr(height), 0x14)
	w.selectPage(w.page)
}
func (a *app) newNativeWindow(title string, width, height int) *nativeWindow {
	h, _, _ := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(utf16("BurnBarControls"))), uintptr(unsafe.Pointer(utf16(title))), windowStyle, 0, 0, 0, 0, a.main, 0, a.hinst, 0)
	if h == 0 {
		a.showMessage("BurnBar", "Could not create the window.")
		return nil
	}
	return &nativeWindow{hwnd: h, width: width, height: height, controls: map[int]nativeControl{}}
}
func closeNative(w **nativeWindow) {
	if *w == nil {
		return
	}
	n := *w
	*w = nil
	pDestroyWindow.Call(n.hwnd)
	if n.font != 0 {
		pDeleteObject.Call(n.font)
	}
}
func (a *app) dialogMessage(m *MSG) bool {
	for _, w := range []*nativeWindow{a.settings, a.accessible} {
		if w == nil {
			continue
		}
		if w == a.accessible && (m.Message == WM_KEYDOWN || m.Message == WM_LBUTTONDOWN) && (m.Hwnd == w.hwnd || isChild(w.hwnd, m.Hwnd)) {
			a.accessibleDeadline = time.Now().Add(a.popupDelay())
		}
		// Handle Escape explicitly because these are registered windows, not resource dialogs.
		if m.Message == WM_KEYDOWN && m.WParam == VK_ESCAPE && (m.Hwnd == w.hwnd || isChild(w.hwnd, m.Hwnd)) {
			pPostMessageW.Call(w.hwnd, WM_CLOSE, 0, 0)
			return true
		}
		r, _, _ := pIsDialogMessageW.Call(w.hwnd, uintptr(unsafe.Pointer(m)))
		if r != 0 {
			return true
		}
	}
	return false
}

var pIsChild = user32.NewProc("IsChild")

func isChild(parent, child uintptr) bool { r, _, _ := pIsChild.Call(parent, child); return r != 0 }
func controlsProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	a := theApp
	if a != nil {
		var w *nativeWindow
		settings := a.settings != nil && a.settings.hwnd == hwnd
		if settings {
			w = a.settings
		} else if a.accessible != nil && a.accessible.hwnd == hwnd {
			w = a.accessible
		}
		if w != nil {
			switch msg {
			case WM_CLOSE:
				if settings {
					closeNative(&a.settings)
				} else {
					closeNative(&a.accessible)
				}
				return 0
			case wmDPIChanged:
				w.layout(int(wparam & 0xffff))
				return 0
			case wmCommand:
				id := int(wparam & 0xffff)
				notification := int(wparam >> 16)
				if settings {
					a.settingsCommand(id, notification)
				} else {
					a.accessibleCommand(id)
				}
				return 0
			}
		}
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}
func copyText(owner uintptr, text string) error {
	chars := []uint16{}
	for _, v := range utf16codec.Encode([]rune(text)) {
		chars = append(chars, v)
	}
	chars = append(chars, 0)
	h, _, _ := pGlobalAlloc.Call(0x42, uintptr(len(chars)*2))
	if h == 0 {
		return fmt.Errorf("clipboard allocation failed")
	}
	ptr, _, _ := pGlobalLock.Call(h)
	if ptr == 0 {
		pGlobalFree.Call(h)
		return fmt.Errorf("clipboard allocation failed")
	}
	kernel32.NewProc("RtlMoveMemory").Call(ptr, uintptr(unsafe.Pointer(&chars[0])), uintptr(len(chars)*2))
	pGlobalUnlock.Call(h)
	opened, _, _ := pOpenClipboard.Call(owner)
	if opened == 0 {
		pGlobalFree.Call(h)
		return fmt.Errorf("clipboard is busy; select the command and press Ctrl+C")
	}
	defer pCloseClipboard.Call()
	pEmptyClipboard.Call()
	ok, _, _ := pSetClipboardData.Call(13, h)
	if ok == 0 {
		pGlobalFree.Call(h)
		return fmt.Errorf("could not copy command")
	}
	return nil
}
