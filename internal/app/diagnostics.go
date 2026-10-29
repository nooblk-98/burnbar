package app

import (
	"encoding/json"
	"io"
	"runtime"
	"unsafe"

	"github.com/nooblk-98/burnbar/internal/layout"
	"github.com/nooblk-98/burnbar/internal/render"
)

// WriteTaskbarLayout reports measured spans and the compact meter's safe slot.
// It reads no element names or text, and does not create windows or fetch usage.
func WriteTaskbarLayout(w io.Writer) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pSetProcessDpiAwarenessContext.Call(DPI_PER_MONITOR_V2)
	uia := newUIA()
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	var meters []map[string]any
	var previous uintptr
	for {
		hwnd, _, _ := pFindWindowExW.Call(0, previous, uintptr(unsafe.Pointer(utf16("BurnBarOverlay"))), 0)
		if hwnd == 0 {
			break
		}
		bounds, _ := windowRect(hwnd)
		visible, _, _ := pIsWindowVisible.Call(hwnd)
		meters = append(meters, map[string]any{"bounds": bounds, "visible": visible != 0})
		previous = hwnd
	}
	for _, tb := range findTaskbars(true) {
		items, readable := uia.items(tb.hwnd)
		occupied, tray, hasTray := layout.AnalyzeTaskbar(items, tb.rect)
		scale := float64(dpiForWindow(tb.hwnd)) / 96
		width, _ := render.WidgetSize(scale)
		x, fits := layout.PickSlot(occupied, int(tb.rect.Left), tray, width, int(6*scale), "auto")
		adaptiveX, adaptiveW, adaptiveFits := layout.PickSizedSlot(occupied, int(tb.rect.Left), tray, width, render.MinimumWidgetWidth(scale), int(6*scale), "auto")
		if err := enc.Encode(map[string]any{"taskbar": tb.rect, "scale": scale, "occupied": occupied, "trayLeft": tray, "readable": readable, "hasTray": hasTray, "meterWidth": width, "meterX": x, "fits": fits, "adaptiveX": adaptiveX, "adaptiveWidth": adaptiveW, "adaptiveFits": adaptiveFits, "visibility": foregroundVisibility(), "meters": meters}); err != nil {
			return err
		}
	}
	return nil
}
