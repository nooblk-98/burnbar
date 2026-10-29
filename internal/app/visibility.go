package app

import (
	"path/filepath"
	"unsafe"

	"github.com/nooblk-98/burnbar/internal/layout"
	"golang.org/x/sys/windows"
)

type foregroundState struct {
	Notification int32  `json:"notificationState"`
	Class        string `json:"class"`
	Process      string `json:"process"`
	Window       RECT   `json:"window"`
	Monitor      RECT   `json:"monitor"`
	Hide         bool   `json:"hideForFullscreen"`
}

func foregroundVisibility() foregroundState {
	var state foregroundState
	pSHQueryUserNotificationState.Call(uintptr(unsafe.Pointer(&state.Notification)))
	hwnd, _, _ := pGetForegroundWindow.Call()
	if hwnd == 0 {
		return state
	}
	state.Window, _ = windowRect(hwnd)
	var class [256]uint16
	pGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class)))
	state.Class = windows.UTF16ToString(class[:])
	var pid uint32
	windows.GetWindowThreadProcessId(windows.HWND(hwnd), &pid)
	if proc, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid); err == nil {
		var path [1024]uint16
		size := uint32(len(path))
		if windows.QueryFullProcessImageName(proc, 0, &path[0], &size) == nil {
			state.Process = filepath.Base(windows.UTF16ToString(path[:size]))
		}
		windows.CloseHandle(proc)
	}
	mon, _, _ := pMonitorFromWindow.Call(hwnd, MONITOR_DEFAULTTONEAR)
	info := MONITORINFO{}
	info.CbSize = uint32(unsafe.Sizeof(info))
	pGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&info)))
	state.Monitor = info.RcMonitor
	state.Hide = layout.HideForFullscreen(state.Notification, state.Class, state.Process, state.Window, state.Monitor)
	return state
}

func fullscreenAppActive() bool { return foregroundVisibility().Hide }
