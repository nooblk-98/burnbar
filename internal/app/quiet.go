package app

import (
	"github.com/nooblk-98/burnbar/internal/alerts"
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
func (a *app) automaticSuppressed() bool {
	if alerts.Quiet(time.Now(), a.cfg.SnoozeUntil, a.cfg.QuietHours, a.cfg.QuietStart, a.cfg.QuietEnd) {
		return true
	}
	state := foregroundVisibility()
	if state.Hide {
		return true
	}
	// QUNS_ACCEPTS_NOTIFICATIONS (5) is the shell's explicit permission to show
	// custom notification UI. Fail closed for unknown/busy/locked/presentation.
	return a.cfg.RespectWindowsQuiet && (state.Notification != 5 || a.focus.active())
}
