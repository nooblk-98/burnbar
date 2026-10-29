package app

import (
	"syscall"
	"unsafe"

	"github.com/nooblk-98/burnbar/internal/layout"

	"golang.org/x/sys/windows"
)

// Minimal UI Automation client used to find where the Windows 11 taskbar draws its buttons,
// so the meter can sit in free space instead of covering app icons.

var (
	ole32    = windows.NewLazySystemDLL("ole32.dll")
	oleaut32 = windows.NewLazySystemDLL("oleaut32.dll")

	pCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	pCoCreateInstance = ole32.NewProc("CoCreateInstance")
	pSysFreeString    = oleaut32.NewProc("SysFreeString")

	clsidCUIAutomation = windows.GUID{Data1: 0xFF48DBA4, Data2: 0x60EF, Data3: 0x4201, Data4: [8]byte{0xAA, 0x87, 0x54, 0x10, 0x3E, 0xEF, 0x59, 0x4E}}
	iidIUIAutomation   = windows.GUID{Data1: 0x30CBE57D, Data2: 0xD9D0, Data3: 0x452A, Data4: [8]byte{0xAB, 0x13, 0x7A, 0xC5, 0xAC, 0x48, 0x25, 0xEE}}
)

const (
	coinitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1
	treeScopeDescendants    = 0x4

	// vtable slots
	vtRelease               = 2
	vtAutoElementFromHandle = 6
	vtAutoCreateTrueCond    = 21
	vtElemFindAll           = 6
	vtElemClassName         = 30
	vtElemControlType       = 21
	vtElemIsOffscreen       = 38
	vtElemBoundingRectangle = 43
	vtArrayLength           = 3
	vtArrayGetElement       = 4
)

type comObj struct{ vtbl *[64]uintptr }

func (o *comObj) call(slot int, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(o.vtbl[slot], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return r
}

func (o *comObj) release() {
	if o != nil {
		o.call(vtRelease)
	}
}

type uiaClient struct {
	auto *comObj
	all  *comObj // true condition
}

// newUIA must be called on the GUI thread (it initialises COM in single-threaded mode).
func newUIA() *uiaClient {
	pCoInitializeEx.Call(0, coinitApartmentThreaded)
	var auto *comObj
	hr, _, _ := pCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidCUIAutomation)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIUIAutomation)), uintptr(unsafe.Pointer(&auto)))
	if hr != 0 || auto == nil {
		return nil
	}
	u := &uiaClient{auto: auto}
	if auto.call(vtAutoCreateTrueCond, uintptr(unsafe.Pointer(&u.all))) != 0 || u.all == nil {
		auto.release()
		return nil
	}
	return u
}

func bstrToString(b *uint16) string {
	if b == nil {
		return ""
	}
	s := windows.UTF16PtrToString(b)
	pSysFreeString.Call(uintptr(unsafe.Pointer(b)))
	return s
}

// items lists the class name and screen rectangle of every visible element inside a taskbar window.
func (u *uiaClient) items(taskbar uintptr) ([]layout.Item, bool) {
	if u == nil {
		return nil, false
	}
	var root *comObj
	if hr := u.auto.call(vtAutoElementFromHandle, taskbar, uintptr(unsafe.Pointer(&root))); hr != 0 || root == nil {
		return nil, false
	}
	defer root.release()

	var arr *comObj
	if hr := root.call(vtElemFindAll, treeScopeDescendants, uintptr(unsafe.Pointer(u.all)), uintptr(unsafe.Pointer(&arr))); hr != 0 || arr == nil {
		return nil, false
	}
	defer arr.release()
	var n int32
	if arr.call(vtArrayLength, uintptr(unsafe.Pointer(&n))) != 0 || n <= 0 {
		return nil, false
	}
	items := make([]layout.Item, 0, n)
	for i := int32(0); i < n; i++ {
		var el *comObj
		if hr := arr.call(vtArrayGetElement, uintptr(i), uintptr(unsafe.Pointer(&el))); hr != 0 || el == nil {
			continue
		}
		var offscreen int32
		if el.call(vtElemIsOffscreen, uintptr(unsafe.Pointer(&offscreen))) == 0 && offscreen != 0 {
			el.release()
			continue
		}
		var r RECT
		if el.call(vtElemBoundingRectangle, uintptr(unsafe.Pointer(&r))) == 0 && r.Right > r.Left {
			var b *uint16
			el.call(vtElemClassName, uintptr(unsafe.Pointer(&b)))
			var controlType int32
			el.call(vtElemControlType, uintptr(unsafe.Pointer(&controlType)))
			items = append(items, layout.Item{Class: bstrToString(b), Rect: r, ControlType: controlType})
		}
		el.release()
	}
	return items, len(items) > 0
}
