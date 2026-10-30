package app

import (
	"fmt"
	"unsafe"
)

const (
	repositoryURL = "https://github.com/nooblk-98/burnbar"
	developerName = "Lahiru S Liyanage (@nooblk-98)"
)

func (a *app) showMessage(title, message string) {
	pMessageBoxW.Call(a.main, uintptr(unsafe.Pointer(utf16(message))),
		uintptr(unsafe.Pointer(utf16(title))), MB_OK|MB_ICONINFORMATION)
}

func (a *app) showAbout() {
	if a.popupVisible {
		a.hidePopup()
	}
	version := a.version
	if version == "" {
		version = "dev"
	}
	a.showMessage("About BurnBar", fmt.Sprintf(
		"BurnBar %s\n\nClaude and Codex usage monitor for the Windows taskbar.\n\nDeveloped by %s\n\nRepository:\n%s\n\nMIT License\n\nAn unofficial community project, not affiliated with Anthropic or OpenAI.",
		version, developerName, repositoryURL))
}

func (a *app) openRepository() {
	r, _, _ := pShellExecuteW.Call(a.main, uintptr(unsafe.Pointer(utf16("open"))),
		uintptr(unsafe.Pointer(utf16(repositoryURL))), 0, 0, SW_SHOWNORMAL)
	if r <= 32 {
		a.showMessage("Open GitHub repository", "Could not open your browser.\n\n"+repositoryURL)
	}
}
