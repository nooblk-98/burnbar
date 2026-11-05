package app

import (
	"fmt"
	"github.com/nooblk-98/burnbar/internal/insights"
	"github.com/nooblk-98/burnbar/internal/render"
	"strings"
	"time"
)

func (a *app) staleAfter() time.Duration { return max(10*time.Minute, 2*a.interval()) }
func (a *app) usageText() string {
	now := time.Now()
	var lines []string
	for _, u := range a.snapshot() {
		lines = append(lines, u.Name()+" usage", insights.Freshness(u, now))
		if health := insights.Health(u, now, a.staleAfter()); health != "" {
			lines = append(lines, health)
			if len(u.All()) > 0 {
				lines = append(lines, "Numbers below are the last successful reading.")
			}
		}
		for _, w := range u.All() {
			lines = append(lines, fmt.Sprintf("%s: %.0f%% used. %s", w.Label, w.Percent, render.ResetText(w.ResetsAt)))
			if a.cfg.ShowForecast && (w == u.FiveHour || w == u.SevenDay) {
				if f, ok := insights.Estimate(u, a.hist[u.Provider].Since(now.Add(-3*time.Hour)), w == u.SevenDay, now); ok {
					lines = append(lines, f.Text())
				}
			}
		}
		if u.Err != "" {
			lines = append(lines, u.Err)
		}
		lines = append(lines, "")
	}
	return strings.Join(lines, "\r\n")
}
func (a *app) showAccessible() {
	if a.popupVisible {
		a.hidePopup()
	}
	if a.accessible == nil {
		w := a.newNativeWindow("BurnBar readable usage", 610, 450)
		if w == nil {
			return
		}
		a.accessible = w
		w.label(3000, "&Usage summary (select text and press Ctrl+C to copy):", 18, 16, 575, -1)
		w.add(3001, "EDIT", a.usageText(), wsTabStop|wsVScroll|0x00800000|0x0004|0x0040|0x0800, 18, 44, 575, 300, -1)
		w.button(1, "&Refresh", 18, 365, 120, -1)
		w.button(3, "&Settings", 155, 365, 120, -1)
		w.button(2, "&Close", 473, 365, 120, -1)
		w.label(3002, "Closes after your popup delay. Keyboard interaction restarts the timer.", 18, 410, 575, -1)
		w.layout(dpiForWindow(a.primaryTaskbar()))
	}
	a.accessibleDeadline = time.Now().Add(a.popupDelay())
	pShowWindow.Call(a.accessible.hwnd, SW_SHOW)
	pSetForegroundWindow.Call(a.accessible.hwnd)
	pSetFocus.Call(a.accessible.controls[3001].hwnd)
}
func (a *app) updateAccessible() {
	if a.accessible != nil {
		if text := a.usageText(); a.accessible.text(3001) != text {
			a.accessible.set(3001, text)
		}
	}
}
func (a *app) accessibleCommand(id int) {
	switch id {
	case 1:
		a.refresh(true)
		a.accessibleDeadline = time.Now().Add(a.popupDelay())
	case 2:
		closeNative(&a.accessible)
	case 3:
		closeNative(&a.accessible)
		a.showSettings()
	}
}
