// Package app is the Windows side of the meter: taskbar overlays, tray icon, popup and timers.
package app

import (
	"errors"
	"runtime"

	"github.com/nooblk-98/burnbar/internal/config"
	"github.com/nooblk-98/burnbar/internal/history"

	"golang.org/x/sys/windows"
)

// Run shows the meter and blocks until the user exits. A second instance exits immediately.
func Run(demo bool) error {
	// Win32 windows belong to the thread that created them; keep the message loop on one OS thread.
	runtime.LockOSThread()

	mutex, err := windows.CreateMutex(nil, false, utf16(`Local\BurnBar`))
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return nil
	}
	defer windows.CloseHandle(mutex)

	a := &app{demo: demo, cfg: config.Load()}
	if demo {
		a.hist = history.Demo()
	} else {
		a.hist = history.Load(config.Dir())
	}
	theApp = a
	return a.run()
}

func windowsCallback(fn func(hwnd, msg, wparam, lparam uintptr) uintptr) uintptr {
	return windows.NewCallback(fn)
}

func windowsCallback7(fn func(a, b, c, d, e, f, g uintptr) uintptr) uintptr {
	return windows.NewCallback(fn)
}
