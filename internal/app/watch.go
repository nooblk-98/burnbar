package app

import (
	"os"
	"path/filepath"
	"time"

	"claude-usage/internal/usage"

	"golang.org/x/sys/windows"
)

// watchActivity calls onChange whenever Claude Code writes to its session logs, at most every
// couple of seconds. It returns false if the folder can't be watched.
func watchActivity(onChange func()) bool {
	dir := filepath.Join(usage.ClaudeDir(), "projects")
	if _, err := os.Stat(dir); err != nil {
		return false
	}
	h, err := windows.FindFirstChangeNotification(dir, true,
		windows.FILE_NOTIFY_CHANGE_LAST_WRITE|windows.FILE_NOTIFY_CHANGE_FILE_NAME)
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
