package app

import (
	"path/filepath"
	"time"

	"github.com/nooblk-98/burnbar/internal/usage"

	"golang.org/x/sys/windows"
)

// watchActivity watches session logs without reacting to the usage probe's own logs.
func watchActivity(provider usage.Provider, onChange func()) bool {
	dir := filepath.Join(usage.ClaudeDir(), "projects")
	if provider == usage.Codex {
		dir = filepath.Join(usage.CodexDir(), "sessions")
	}
	h, err := windows.FindFirstChangeNotification(dir, true,
		windows.FILE_NOTIFY_CHANGE_LAST_WRITE|windows.FILE_NOTIFY_CHANGE_FILE_NAME|windows.FILE_NOTIFY_CHANGE_DIR_NAME)
	if err != nil {
		return false
	}
	go func() {
		defer windows.FindCloseChangeNotification(h)
		for {
			if ev, err := windows.WaitForSingleObject(h, windows.INFINITE); err != nil || ev != windows.WAIT_OBJECT_0 {
				return
			}
			onChange()
			time.Sleep(2 * time.Second)
			if windows.FindNextChangeNotification(h) != nil {
				return
			}
		}
	}()
	return true
}
