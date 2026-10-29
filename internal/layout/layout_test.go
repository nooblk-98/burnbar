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
	}{
		{"few apps: next to tray", []Rect{{Left: 400, Right: 900}}, "auto", 1414 - 7 - 155},
		{"tray gap too small: left gap", centred, "auto", 198 + 7},
		{"no gap fits: centre in widest", []Rect{{Left: 0, Right: 1300}, {Left: 1350, Right: 1414}}, "auto", 1300 + (50-155)/2},
		{"left mode: leftmost gap", []Rect{{Left: 400, Right: 900}}, "left", 0 + 7},
		{"right mode: always by tray", centred, "right", 1414 - 7 - 155},
	}
	for _, tt := range tests {
		occ := append([]Rect(nil), tt.occupied...)
		if got := PickSlot(occ, 0, 1414, 155, 7, tt.mode); got != tt.want {
			t.Errorf("%s: got %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestAnalyze(t *testing.T) {
	items := []Item{
		{"ToggleButton", Rect{Left: 400, Right: 456}},
		{"Taskbar.TaskListButtonAutomationPeer", Rect{Left: 791, Right: 846}},
		{"TextBlock", Rect{Left: 505, Right: 557}},
		{"SystemTray.NormalButton", Rect{Left: 1413, Right: 1453}},
		{"SystemTray.OmniButton", Rect{Left: 1813, Right: 1900}},
	}
	occ, trayLeft, ok := Analyze(items)
	if !ok || trayLeft != 1413 || len(occ) != 2 {
		t.Fatalf("got occupied=%v trayLeft=%d ok=%v", occ, trayLeft, ok)
	}
}
