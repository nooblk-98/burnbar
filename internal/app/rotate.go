package app

import (
	"image"
	"time"
	"unsafe"

	"github.com/nooblk-98/burnbar/internal/render"
)

const (
	rotateEvery  = 8 * time.Second
	flipDuration = 420 * time.Millisecond
)

func (a *app) resetRotation() {
	a.activeProvider = 0
	a.flipStarted = time.Time{}
	pKillTimer.Call(a.main, timerRotate)
	pKillTimer.Call(a.main, timerFlip)
	if len(a.snapshot()) > 1 {
		pSetTimer.Call(a.main, timerRotate, uintptr(rotateEvery.Milliseconds()), 0)
	}
}

func (a *app) rotateProvider() {
	readings := a.snapshot()
	if len(readings) < 2 || a.popupVisible || !a.flipStarted.IsZero() {
		return
	}
	visible := false
	for _, ov := range a.overlays {
		if ov.hover {
			return
		} // hold the current reading while it is being inspected
		visible = visible || ov.last.visible
	}
	a.flipPrevious = a.activeProvider
	a.activeProvider = (a.activeProvider + 1) % len(readings)
	// Respect Windows' animation preference.
	var animated int32
	r, _, _ := pSystemParametersInfoW.Call(0x1042, 0, uintptr(unsafe.Pointer(&animated)), 0)
	if visible && r != 0 && animated != 0 {
		a.flipStarted = time.Now()
		pSetTimer.Call(a.main, timerFlip, 16, 0)
	}
	a.place(true)
}

func (a *app) widget(ov *overlay, p placement) *image.RGBA {
	readings := a.snapshot()
	if len(readings) == 0 {
		return render.GroupWidget(p.scale, nil, render.NewTheme(p.light), ov.hover)
	}
	active := a.activeProvider % len(readings)
	if a.flipStarted.IsZero() {
		return render.ProviderWidgetSized(p.scale, readings[active], render.NewTheme(p.light), ov.hover, p.w)
	}
	if ov.flipAt != a.flipStarted || ov.flipScale != p.scale || ov.flipLight != p.light || ov.flipWidth != p.w {
		ov.flipFrom = render.ProviderWidgetSized(p.scale, readings[a.flipPrevious%len(readings)], render.NewTheme(p.light), false, p.w)
		ov.flipTo = render.ProviderWidgetSized(p.scale, readings[active], render.NewTheme(p.light), false, p.w)
		ov.flipAt, ov.flipScale, ov.flipLight = a.flipStarted, p.scale, p.light
		ov.flipWidth = p.w
	}
	return render.FlipDown(ov.flipFrom, ov.flipTo, float64(time.Since(a.flipStarted))/float64(flipDuration))
}

func (a *app) animateFlip() {
	if a.flipStarted.IsZero() || time.Since(a.flipStarted) >= flipDuration {
		a.flipStarted = time.Time{}
		pKillTimer.Call(a.main, timerFlip)
		a.place(true)
		return
	}
	for _, ov := range a.overlays {
		if ov.last.visible {
			img := a.widget(ov, ov.last)
			if ov.last.floating {
				img = render.FloatingWidget(img, render.NewTheme(ov.last.light), ov.last.scale)
			}
			a.paint(ov.hwnd, img, ov.last.x, ov.last.y)
		}
	}
}
