package app

import (
	"encoding/json"
	"io"
	"runtime"

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
	for _, tb := range findTaskbars(true) {
		items, readable := uia.items(tb.hwnd)
		occupied, tray, hasTray := layout.Analyze(items)
		scale := float64(dpiForWindow(tb.hwnd)) / 96
		width, _ := render.WidgetSize(scale)
		x, fits := layout.PickSlot(occupied, int(tb.rect.Left), tray, width, int(6*scale), "auto")
		if err := enc.Encode(map[string]any{"taskbar": tb.rect, "scale": scale, "occupied": occupied, "trayLeft": tray, "readable": readable, "hasTray": hasTray, "meterWidth": width, "meterX": x, "fits": fits}); err != nil {
			return err
		}
	}
	return nil
}
