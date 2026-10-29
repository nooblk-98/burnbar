package layout

import "strings"

// HideForFullscreen distinguishes actual fullscreen content from shell surfaces
// such as Search. Notification "busy" alone is not evidence of a fullscreen app.
func HideForFullscreen(notificationState int32, class, process string, window, monitor Rect) bool {
	switch class {
	case "Shell_TrayWnd", "Shell_SecondaryTrayWnd", "Progman", "WorkerW":
		return false
	}
	switch strings.ToLower(process) {
	case "searchhost.exe", "searchapp.exe", "searchui.exe", "startmenuexperiencehost.exe", "shellexperiencehost.exe":
		return false
	}
	if notificationState == 3 || notificationState == 4 { // exclusive D3D or presentation mode
		return true
	}
	if window.Right <= window.Left || window.Bottom <= window.Top || monitor.Right <= monitor.Left || monitor.Bottom <= monitor.Top {
		return false
	}
	return window.Left <= monitor.Left && window.Top <= monitor.Top && window.Right >= monitor.Right && window.Bottom >= monitor.Bottom
}
