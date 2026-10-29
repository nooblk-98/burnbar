package app

import (
	"syscall"
	"unsafe"

	"github.com/nooblk-98/burnbar/internal/layout"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	pRegisterClassExW              = user32.NewProc("RegisterClassExW")
	pCreateWindowExW               = user32.NewProc("CreateWindowExW")
	pDefWindowProcW                = user32.NewProc("DefWindowProcW")
	pGetMessageW                   = user32.NewProc("GetMessageW")
	pTranslateMessage              = user32.NewProc("TranslateMessage")
	pDispatchMessageW              = user32.NewProc("DispatchMessageW")
	pPostQuitMessage               = user32.NewProc("PostQuitMessage")
	pPostMessageW                  = user32.NewProc("PostMessageW")
	pFindWindowW                   = user32.NewProc("FindWindowW")
	pFindWindowExW                 = user32.NewProc("FindWindowExW")
	pGetWindowRect                 = user32.NewProc("GetWindowRect")
	pIsWindowVisible               = user32.NewProc("IsWindowVisible")
	pSetWindowPos                  = user32.NewProc("SetWindowPos")
	pShowWindow                    = user32.NewProc("ShowWindow")
	pSetTimer                      = user32.NewProc("SetTimer")
	pGetDC                         = user32.NewProc("GetDC")
	pReleaseDC                     = user32.NewProc("ReleaseDC")
	pUpdateLayeredWindow           = user32.NewProc("UpdateLayeredWindow")
	pCreatePopupMenu               = user32.NewProc("CreatePopupMenu")
	pAppendMenuW                   = user32.NewProc("AppendMenuW")
	pTrackPopupMenu                = user32.NewProc("TrackPopupMenu")
	pDestroyMenu                   = user32.NewProc("DestroyMenu")
	pSetForegroundWindow           = user32.NewProc("SetForegroundWindow")
	pGetCursorPos                  = user32.NewProc("GetCursorPos")
	pCreateIconIndirect            = user32.NewProc("CreateIconIndirect")
	pDestroyIcon                   = user32.NewProc("DestroyIcon")
	pLoadCursorW                   = user32.NewProc("LoadCursorW")
	pRegisterWindowMessageW        = user32.NewProc("RegisterWindowMessageW")
	pSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	pGetDpiForWindow               = user32.NewProc("GetDpiForWindow")
	pGetSystemMetricsForDpi        = user32.NewProc("GetSystemMetricsForDpi")
	pTrackMouseEvent               = user32.NewProc("TrackMouseEvent")
	pSetWinEventHook               = user32.NewProc("SetWinEventHook")
	pKillTimer                     = user32.NewProc("KillTimer")
	pDestroyWindow                 = user32.NewProc("DestroyWindow")
	pMonitorFromWindow             = user32.NewProc("MonitorFromWindow")
	pGetMonitorInfoW               = user32.NewProc("GetMonitorInfoW")
	pLoadImageW                    = user32.NewProc("LoadImageW")

	pCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	pCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	pCreateBitmap       = gdi32.NewProc("CreateBitmap")
	pSelectObject       = gdi32.NewProc("SelectObject")
	pDeleteObject       = gdi32.NewProc("DeleteObject")
	pDeleteDC           = gdi32.NewProc("DeleteDC")

	pShellNotifyIconW             = shell32.NewProc("Shell_NotifyIconW")
	pSHQueryUserNotificationState = shell32.NewProc("SHQueryUserNotificationState")

	pGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
)

const (
	WS_POPUP            = 0x80000000
	WS_EX_LAYERED       = 0x00080000
	WS_EX_TOOLWINDOW    = 0x00000080
	WS_EX_TOPMOST       = 0x00000008
	WS_EX_NOACTIVATE    = 0x08000000
	WM_DESTROY          = 0x0002
	WM_TIMER            = 0x0113
	WM_COMMAND          = 0x0111
	WM_MOUSEMOVE        = 0x0200
	WM_LBUTTONUP        = 0x0202
	WM_RBUTTONUP        = 0x0205
	WM_MOUSELEAVE       = 0x02A3
	WM_NULL             = 0x0000
	WM_APP              = 0x8000
	SW_HIDE             = 0
	SW_SHOWNOACTIVATE   = 4
	SWP_NOSIZE          = 0x0001
	SWP_NOMOVE          = 0x0002
	SWP_NOACTIVATE      = 0x0010
	HWND_TOPMOST        = ^uintptr(0) // (HWND)-1
	ULW_ALPHA           = 0x02
	AC_SRC_ALPHA        = 0x01
	MF_STRING           = 0x0000
	MF_CHECKED          = 0x0008
	MF_GRAYED           = 0x0001
	MF_SEPARATOR        = 0x0800
	TPM_RETURNCMD       = 0x0100
	TPM_RIGHTBUTTON     = 0x0002
	TPM_BOTTOMALIGN     = 0x0020
	NIM_ADD             = 0
	NIM_MODIFY          = 1
	NIM_DELETE          = 2
	NIF_MESSAGE         = 0x01
	NIF_ICON            = 0x02
	NIF_TIP             = 0x04
	TME_LEAVE           = 0x02
	SM_CXSMICON         = 49
	IDC_ARROW           = 32512
	EVENT_SYS_FOREGND   = 0x0003
	WINEVENT_OUTOFCTX   = 0x0000
	DPI_PER_MONITOR_V2  = ^uintptr(3) // (DPI_AWARENESS_CONTEXT)-4
	QUNS_BUSY           = 2
	QUNS_D3D_FULLSCREEN = 3
	QUNS_PRESENTATION   = 4

	WM_ACTIVATE             = 0x0006
	WM_KEYDOWN              = 0x0100
	WA_INACTIVE             = 0
	VK_ESCAPE               = 0x1B
	SW_SHOW                 = 5
	MF_POPUP                = 0x0010
	NIF_INFO                = 0x10
	NIIF_INFO               = 0x1
	NIIF_WARNING            = 0x2
	NIIF_RESPECT_QUIET_TIME = 0x80
	EVENT_OBJECT_DESTROY    = 0x8001
	EVENT_OBJECT_HIDE       = 0x8003
	WINEVENT_SKIPOWNPROCESS = 0x0002
	OBJID_WINDOW            = 0
	MONITOR_DEFAULTTONEAR   = 2
	IMAGE_ICON              = 1
	LR_DEFAULTSIZE          = 0x40
)

type MONITORINFO struct {
	CbSize    uint32
	RcMonitor RECT
	RcWork    RECT
	DwFlags   uint32
}

func workArea(hwnd uintptr) RECT {
	mon, _, _ := pMonitorFromWindow.Call(hwnd, MONITOR_DEFAULTTONEAR)
	mi := MONITORINFO{}
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	pGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
	return mi.RcWork
}

type POINT struct{ X, Y int32 }
type SIZE struct{ CX, CY int32 }

// RECT is layout.Rect, which has the Win32 RECT memory layout.
type RECT = layout.Rect

type MSG struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       POINT
	LPrivate uint32
}

type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type BITMAPINFOHEADER struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type BITMAPINFO struct {
	Header BITMAPINFOHEADER
	Colors [1]uint32
}

type BLENDFUNCTION struct {
	BlendOp, BlendFlags, SourceConstantAlpha, AlphaFormat byte
}

type ICONINFO struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	HbmMask  uintptr
	HbmColor uintptr
}

type NOTIFYICONDATAW struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     uintptr
}

type TRACKMOUSEEVENT struct {
	CbSize      uint32
	DwFlags     uint32
	HwndTrack   uintptr
	DwHoverTime uint32
}

func utf16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func copyUTF16(dst []uint16, s string) {
	u, _ := syscall.UTF16FromString(s)
	if len(u) > len(dst) {
		u = u[:len(dst)-1]
		u = append(u, 0)
	}
	copy(dst, u)
}

func windowRect(hwnd uintptr) (RECT, bool) {
	var r RECT
	ok, _, _ := pGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r, ok != 0
}

func dpiForWindow(hwnd uintptr) int {
	if pGetDpiForWindow.Find() != nil || hwnd == 0 {
		return 96
	}
	d, _, _ := pGetDpiForWindow.Call(hwnd)
	if d == 0 {
		return 96
	}
	return int(d)
}

// newDIB creates a top-down 32-bit BGRA DIB section and returns it with a view of its pixels.
func newDIB(w, h int) (uintptr, []byte) {
	bi := BITMAPINFO{Header: BITMAPINFOHEADER{
		Size:     uint32(unsafe.Sizeof(BITMAPINFOHEADER{})),
		Width:    int32(w),
		Height:   -int32(h),
		Planes:   1,
		BitCount: 32,
	}}
	var bits unsafe.Pointer
	hbm, _, _ := pCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hbm == 0 || bits == nil {
		return 0, nil
	}
	return hbm, unsafe.Slice((*byte)(bits), w*h*4)
}
