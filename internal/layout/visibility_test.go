package layout

import "testing"

func TestSearchAndOrdinaryWindowsRemainVisible(t *testing.T) {
	monitor := Rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
	maximized := Rect{Left: -8, Top: -8, Right: 1928, Bottom: 1028}
	for _, tc := range []struct {
		name, class, process string
		state                int32
		window               Rect
		hide                 bool
	}{
		{"search busy", "Windows.UI.Core.CoreWindow", "SearchHost.exe", 2, monitor, false},
		{"search over game", "Windows.UI.Core.CoreWindow", "SearchHost.exe", 3, monitor, false},
		{"start menu", "Windows.UI.Core.CoreWindow", "StartMenuExperienceHost.exe", 2, monitor, false},
		{"desktop", "Progman", "explorer.exe", 2, monitor, false},
		{"desktop worker", "WorkerW", "explorer.exe", 5, monitor, false},
		{"taskbar click", "Shell_TrayWnd", "explorer.exe", 2, monitor, false},
		{"maximized browser busy", "Chrome_WidgetWin_1", "chrome.exe", 2, maximized, false},
		{"fullscreen browser", "Chrome_WidgetWin_1", "chrome.exe", 5, monitor, true},
		{"fullscreen explorer", "CabinetWClass", "explorer.exe", 5, monitor, true},
		{"exclusive game", "GameWindow", "game.exe", 3, maximized, true},
		{"presentation", "PPTFrameClass", "powerpnt.exe", 4, maximized, true},
		{"invalid foreground", "", "", 2, Rect{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := HideForFullscreen(tc.state, tc.class, tc.process, tc.window, monitor); got != tc.hide {
				t.Fatalf("hide=%v, want %v", got, tc.hide)
			}
		})
	}
}

func TestAnalyzeTaskbarIgnoresSearchAndTrayFlyouts(t *testing.T) {
	bounds := Rect{Left: 0, Top: 1020, Right: 1920, Bottom: 1080}
	items := []Item{
		{Class: "ToggleButton", Rect: Rect{Left: 8, Top: 1020, Right: 198, Bottom: 1080}},
		{Class: "Taskbar.TaskListButtonAutomationPeer", Rect: Rect{Left: 415, Top: 1020, Right: 1301, Bottom: 1080}},
		{Class: "SystemTray.NormalButton", Rect: Rect{Left: 1373, Top: 1020, Right: 1450, Bottom: 1080}},
		// Search controls span the meter's x but sit above the taskbar.
		{Class: "Button", Rect: Rect{Left: 0, Top: 300, Right: 800, Bottom: 1015}, ControlType: 50000},
		{Class: "SystemTray.FlyoutButton", Rect: Rect{Left: 100, Top: 600, Right: 800, Bottom: 1020}},
		// Another monitor's controls cannot affect this taskbar.
		{Class: "SystemTray.NormalButton", Rect: Rect{Left: -1920, Top: 1020, Right: -1800, Bottom: 1080}},
	}
	occupied, tray, ok := AnalyzeTaskbar(items, bounds)
	if !ok || tray != 1373 || len(occupied) != 2 {
		t.Fatalf("occupied=%v tray=%d valid=%v", occupied, tray, ok)
	}
	x, fits := PickSlot(occupied, 0, tray, 163, 7, "auto")
	if !fits || x != 205 {
		t.Fatalf("Search should leave the existing slot available: x=%d fits=%v", x, fits)
	}
}

func TestMeterNarrowsWhenTaskbarButtonsShift(t *testing.T) {
	occupied := []Rect{{Left: 8, Right: 198}, {Left: 360, Right: 1301}}
	if _, fits := PickSlot(occupied, 0, 1373, 163, 7, "auto"); fits {
		t.Fatal("the original width must not fit this regression case")
	}
	x, width, fits := PickSizedSlot(occupied, 0, 1373, 163, 120, 7, "auto")
	if !fits || x != 205 || width != 148 {
		t.Fatalf("narrowed slot: x=%d width=%d fits=%v", x, width, fits)
	}
	if x+width+7 > 360 {
		t.Fatal("narrowed widget overlaps Start")
	}
	if _, _, fits := PickSizedSlot([]Rect{{Left: 0, Right: 1373}}, 0, 1373, 163, 120, 7, "auto"); fits {
		t.Fatal("a full taskbar must use the floating fallback")
	}
}
