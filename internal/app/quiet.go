package app

import (
	"github.com/nooblk-98/burnbar/internal/alerts"
	"github.com/nooblk-98/burnbar/internal/layout"
	"golang.org/x/sys/windows"
	"syscall"
	"time"
	"unsafe"
)

// Read-only WinRT adapter for Windows.UI.Shell.FocusSessionManager. Interfaces
// and vtable layout come from Microsoft's Windows SDK metadata/windows-rs.
// Activation failure on older Windows versions falls back to the shell state.
var combase = windows.NewLazySystemDLL("combase.dll")
var pRoInitialize = combase.NewProc("RoInitialize")
var pRoUninitialize = combase.NewProc("RoUninitialize")
var pWindowsCreateString = combase.NewProc("WindowsCreateString")
var pWindowsDeleteString = combase.NewProc("WindowsDeleteString")
var pRoGetActivationFactory = combase.NewProc("RoGetActivationFactory")

type focusReader struct {
	manager     *comObject
	initialized bool
}

type comObject struct{ Vtbl *[16]uintptr }

func releaseCOM(ptr *comObject) { syscall.SyscallN(ptr.Vtbl[2], uintptr(unsafe.Pointer(ptr))) }
func newFocusReader() *focusReader {
	f := &focusReader{}
	hr, _, _ := pRoInitialize.Call(0)
	if int32(hr) < 0 {
		return f
	}
	f.initialized = true
	name := windows.StringToUTF16("Windows.UI.Shell.FocusSessionManager")
	var h uintptr
	hr, _, _ = pWindowsCreateString.Call(uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)-1), uintptr(unsafe.Pointer(&h)))
	if int32(hr) < 0 {
		return f
	}
	defer pWindowsDeleteString.Call(h)
	iid := windows.GUID{Data1: 0x834df764, Data2: 0xcb9a, Data3: 0x5d0a, Data4: [8]byte{0xaa, 0x9f, 0x73, 0xdf, 0x4f, 0x24, 0x93, 0x95}}
	var factory *comObject
	hr, _, _ = pRoGetActivationFactory.Call(h, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&factory)))
	if int32(hr) < 0 || factory == nil {
		return f
	}
	defer releaseCOM(factory)
	var supported byte
	if hr, _, _ := syscall.SyscallN(factory.Vtbl[7], uintptr(unsafe.Pointer(factory)), uintptr(unsafe.Pointer(&supported))); int32(hr) < 0 || supported == 0 {
		return f
	}
	if hr, _, _ := syscall.SyscallN(factory.Vtbl[6], uintptr(unsafe.Pointer(factory)), uintptr(unsafe.Pointer(&f.manager))); int32(hr) < 0 {
		f.manager = nil
	}
	return f
}
func (f *focusReader) active() bool {
	if f == nil || f.manager == nil {
		return false
	}
	var active byte
	hr, _, _ := syscall.SyscallN(f.manager.Vtbl[6], uintptr(unsafe.Pointer(f.manager)), uintptr(unsafe.Pointer(&active)))
	return int32(hr) >= 0 && active != 0
}
func (f *focusReader) close() {
	if f == nil {
		return
	}
	if f.manager != nil {
		releaseCOM(f.manager)
		f.manager = nil
	}
	if f.initialized {
		pRoUninitialize.Call()
		f.initialized = false
	}
}

// userQuiet reports BurnBar's own quiet controls: snooze and scheduled quiet hours.
func (a *app) userQuiet() bool {
	return alerts.Quiet(time.Now(), a.cfg.SnoozeUntil, a.cfg.QuietHours, a.cfg.QuietStart, a.cfg.QuietEnd)
}

// nativeSuppressed gates Windows balloon/toast notices. They are sent with
// NIIF_RESPECT_QUIET_TIME, so Windows applies its own quiet time, Focus and
// Do Not Disturb rules; second-guessing them from shell state here would drop
// 80%/95% alerts that Windows would otherwise deliver or file in Action Center.
func (a *app) nativeSuppressed() bool { return a.userQuiet() }

// automaticSuppressed gates BurnBar's own custom cards, which Windows does not filter.
func (a *app) automaticSuppressed() bool {
	if a.userQuiet() {
		return true
	}
	state := foregroundVisibility()
	if state.Hide {
		return true
	}
	// Only states that positively indicate quiet suppress cards. QUNS_BUSY and
	// unknown/failed queries do not: busy is reported outside fullscreen too,
	// and real fullscreen is already covered by state.Hide above.
	return a.cfg.RespectWindowsQuiet && (layout.ShellQuiet(state.Notification) || a.focus.active())
}
