package app

import "unsafe"

type taskbar struct {
	hwnd    uintptr
	rect    RECT
	primary bool
}

// findTaskbars returns the primary taskbar and, if wanted, the taskbars on other monitors.
func findTaskbars(all bool) []taskbar {
	var out []taskbar
	if tb, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(utf16("Shell_TrayWnd"))), 0); tb != 0 {
		if r, ok := windowRect(tb); ok {
			out = append(out, taskbar{tb, r, true})
		}
	}
	if !all {
		return out
	}
	var prev uintptr
	for {
		tb, _, _ := pFindWindowExW.Call(0, prev, uintptr(unsafe.Pointer(utf16("Shell_SecondaryTrayWnd"))), 0)
		if tb == 0 {
			break
		}
		if r, ok := windowRect(tb); ok {
			out = append(out, taskbar{tb, r, false})
		}
		prev = tb
	}
	return out
}
