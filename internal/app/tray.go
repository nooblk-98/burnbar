package app

import "unsafe"

type trayAction uintptr

const (
	trayNoAction trayAction = iota
	trayDetails
	trayMenu
)

type trayInput struct {
	pressed bool
	dragged bool
	origin  POINT
	drag    SIZE
}

func (t *trayInput) moved(p POINT) {
	if !t.pressed {
		return
	}
	dx, dy := p.X-t.origin.X, p.Y-t.origin.Y
	if dx < -t.drag.CX || dx > t.drag.CX || dy < -t.drag.CY || dy > t.drag.CY {
		t.dragged = true
	}
}

// action distinguishes a shell icon selection from the mouse-up ending a drag.
func (t *trayInput) action(event uintptr, p POINT, drag SIZE, modern bool) trayAction {
	switch event {
	case WM_LBUTTONDOWN:
		*t = trayInput{pressed: true, origin: p, drag: drag}
	case WM_MOUSEMOVE:
		t.moved(p)
	case WM_LBUTTONUP:
		t.moved(p)
		if !modern {
			if !t.pressed {
				return trayNoAction
			}
			return t.finish()
		}
	case NIN_SELECT:
		if modern {
			t.moved(p)
			return t.finish()
		}
	case NIN_KEYSELECT:
		if modern {
			*t = trayInput{}
			return trayDetails
		}
	case WM_CONTEXTMENU:
		if modern {
			*t = trayInput{}
			return trayMenu
		}
	case WM_RBUTTONUP:
		if !modern {
			*t = trayInput{}
			return trayMenu
		}
	}
	return trayNoAction
}

func (t *trayInput) finish() trayAction {
	dragged := t.dragged
	*t = trayInput{}
	if dragged {
		return trayNoAction
	}
	return trayDetails
}

func packTrayPoint(p POINT) uintptr {
	return uintptr(uint32(uint16(p.X)) | uint32(uint16(p.Y))<<16)
}

func unpackTrayPoint(p uintptr) POINT {
	return POINT{X: int32(int16(p & 0xFFFF)), Y: int32(int16((p >> 16) & 0xFFFF))}
}

// handleTray only queues work. Never render, query Explorer, or enter a modal
// menu loop while Explorer is delivering a notification or finishing a drag.
func (a *app) handleTray(wparam, lparam uintptr) {
	event := lparam & 0xFFFF
	var p POINT
	if a.trayVersion4 {
		p = unpackTrayPoint(wparam)
	} else {
		pGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	}
	var drag SIZE
	if event == WM_LBUTTONDOWN {
		x, _, _ := pGetSystemMetrics.Call(SM_CXDRAG)
		y, _, _ := pGetSystemMetrics.Call(SM_CYDRAG)
		drag = SIZE{CX: max(1, int32(x)), CY: max(1, int32(y))}
	}
	if action := a.trayInput.action(event, p, drag, a.trayVersion4); action != trayNoAction {
		pPostMessageW.Call(a.main, msgTrayAction, uintptr(action), packTrayPoint(p))
	}
}
