package app

import (
	"context"
	"fmt"
	"github.com/nooblk-98/burnbar/internal/updates"
	"net/http"
	"strings"
	"time"
	"unsafe"
)

func (a *app) updateStatus() string {
	if a.updateBusy {
		return "Checking GitHub for stable updates…"
	}
	if a.updateError != "" {
		return a.updateError
	}
	if a.latestRelease.Version == "" {
		return "Current version: " + a.version + ". Not checked yet."
	}
	if updates.Newer(a.version, a.latestRelease.Version) {
		return "Update available: " + a.latestRelease.Version + " (current " + a.version + ")"
	}
	if strings.Contains(a.version, "-") || a.version == "dev" {
		return "Development build. Latest stable: " + a.latestRelease.Version
	}
	return "Current version: " + a.version + ". Latest stable: " + a.latestRelease.Version
}
func (a *app) releaseURL() string {
	if a.latestRelease.URL != "" {
		return a.latestRelease.URL
	}
	return updates.Releases
}
func (a *app) openURL(url string) {
	r, _, _ := pShellExecuteW.Call(a.main, uintptr(unsafe.Pointer(utf16("open"))), uintptr(unsafe.Pointer(utf16(url))), 0, 0, SW_SHOWNORMAL)
	if r <= 32 {
		a.showMessage("Open link", "Could not open your browser.\n\n"+url)
	}
}
func (a *app) checkUpdates(manual bool) {
	if a.updateBusy || a.demo && !manual {
		return
	}
	now := time.Now()
	if !manual && (!a.cfg.CheckUpdates || now.Sub(a.lastUpdateCheck) < 24*time.Hour) {
		return
	}
	if manual && now.Sub(a.lastUpdateCheck) < time.Minute {
		a.updateError = "Please wait a minute before checking again."
		if a.settings != nil {
			a.settings.set(settingUpdateStatus, a.updateStatus())
		}
		return
	}
	a.updateBusy = true
	a.lastUpdateCheck = now
	a.updateError = ""
	if a.settings != nil {
		a.settings.set(settingUpdateStatus, a.updateStatus())
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		release, err := updates.Check(ctx, &http.Client{Timeout: 15 * time.Second})
		a.mu.Lock()
		a.pendingRelease = release
		a.pendingUpdateError = err
		a.mu.Unlock()
		pPostMessageW.Call(a.main, msgUpdate, 0, 0)
	}()
}
func (a *app) onUpdate() {
	a.mu.Lock()
	release, err := a.pendingRelease, a.pendingUpdateError
	a.mu.Unlock()
	a.updateBusy = false
	if err != nil {
		a.updateError = fmt.Sprintf("Update check failed: %v", err)
	} else {
		a.latestRelease = release
		a.updateError = ""
	}
	if a.settings != nil {
		a.settings.set(settingUpdateStatus, a.updateStatus())
	}
	if err == nil && a.cfg.CheckUpdates && updates.Newer(a.version, release.Version) && a.updateNotified != release.Version && !a.nativeSuppressed() {
		a.notify("BurnBar "+release.Version+" is available", "Open Settings > Updates for release notes and the update command.", false)
		a.updateNotified = release.Version
	}
}
