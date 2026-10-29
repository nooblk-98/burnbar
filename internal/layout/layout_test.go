package layout

import "testing"

func TestPickSlot(t *testing.T) {
	// Centred Windows 11 taskbar at 125% scaling, 1920px wide, tray starting at 1414.
	centred := []Rect{
		{Left: 8, Right: 198},    // Widgets
		{Left: 400, Right: 456},  // Start
		{Left: 458, Right: 732},  // Search
		{Left: 736, Right: 791},  // Task View
		{Left: 791, Right: 1341}, // running apps
	}
	tests := []struct {
		name     string
		occupied []Rect
		mode     string
		want     int
		ok       bool
	}{
		{"few apps: next to tray", []Rect{{Left: 400, Right: 900}}, "auto", 1414 - 7 - 155, true},
		{"tray gap too small: left gap", centred, "auto", 198 + 7, true},
		{"no gap fits: tray only", []Rect{{Left: 0, Right: 1300}, {Left: 1350, Right: 1414}}, "auto", 0, false},
		{"left mode: leftmost gap", []Rect{{Left: 400, Right: 900}}, "left", 0 + 7, true},
		{"right mode: avoid occupied tray gap", centred, "right", 198 + 7, true},
		{"fully occupied", []Rect{{Left: 0, Right: 1414}}, "auto", 0, false},
		{"off-screen items ignored", []Rect{{Left: -200, Right: -10}, {Left: 1500, Right: 2000}}, "auto", 1414 - 7 - 155, true},
	}
	for _, tt := range tests {
		occ := append([]Rect(nil), tt.occupied...)
		if got, ok := PickSlot(occ, 0, 1414, 155, 7, tt.mode); got != tt.want || ok != tt.ok {
			t.Errorf("%s: got %d/%v, want %d/%v", tt.name, got, ok, tt.want, tt.ok)
		}
	}
}

func TestAnalyze(t *testing.T) {
	items := []Item{
		{Class: "ToggleButton", Rect: Rect{Left: 400, Right: 456}},
		{Class: "Taskbar.TaskListButtonAutomationPeer", Rect: Rect{Left: 791, Right: 846}},
		{Class: "TextBlock", Rect: Rect{Left: 505, Right: 557}},
		{Class: "SystemTray.NormalButton", Rect: Rect{Left: 1413, Right: 1453}},
		{Class: "SystemTray.OmniButton", Rect: Rect{Left: 1813, Right: 1900}},
		{Class: "WidgetsButton", Rect: Rect{Left: 8, Right: 198}, ControlType: 50000},
		{Class: "SearchInput", Rect: Rect{Left: 458, Right: 732}, ControlType: 50004},
	}
	occ, trayLeft, ok := Analyze(items)
	if !ok || trayLeft != 1413 || len(occ) != 4 {
		t.Fatalf("got occupied=%v trayLeft=%d ok=%v", occ, trayLeft, ok)
	}
}
