package app

import "testing"

func TestTraySelectionDoesNotActivateAfterDrag(t *testing.T) {
	for _, modern := range []bool{false, true} {
		var input trayInput
		drag := SIZE{CX: 4, CY: 4}
		start := POINT{X: 1500, Y: 900}
		input.action(WM_LBUTTONDOWN, start, drag, modern)
		input.action(WM_MOUSEMOVE, POINT{X: 1600, Y: 1050}, drag, modern)
		// Moving back does not turn an already-started drag into a click.
		if action := input.action(WM_LBUTTONUP, start, drag, modern); action != trayNoAction {
			t.Fatalf("modern=%v: a drag opened the details panel", modern)
		}
		if modern && input.action(NIN_SELECT, start, drag, modern) != trayNoAction {
			t.Fatal("version 4 selection after dragging opened the details panel")
		}
		// A later ordinary click still works.
		input.action(WM_LBUTTONDOWN, start, drag, modern)
		up := input.action(WM_LBUTTONUP, POINT{X: start.X + 1, Y: start.Y}, drag, modern)
		if modern {
			if up != trayNoAction || input.action(NIN_SELECT, start, drag, true) != trayDetails {
				t.Fatal("modern clicks must activate once via NIN_SELECT")
			}
		} else if up != trayDetails {
			t.Fatal("legacy click did not activate")
		}
	}
}

func TestTrayDropWithoutMoveCallbacksIsIgnored(t *testing.T) {
	for _, modern := range []bool{false, true} {
		var input trayInput
		input.action(WM_LBUTTONDOWN, POINT{X: 1700, Y: 850}, SIZE{CX: 4, CY: 4}, modern)
		if input.action(WM_LBUTTONUP, POINT{X: 1700, Y: 1050}, SIZE{}, modern) != trayNoAction {
			t.Fatal("dropping outside the origin must not open the panel")
		}
		if modern && input.action(NIN_SELECT, POINT{X: 1700, Y: 1050}, SIZE{}, modern) != trayNoAction {
			t.Fatal("selection after a distant drop must not open the panel")
		}
	}
}

func TestTrayKeyboardAndContextMenu(t *testing.T) {
	var input trayInput
	if input.action(WM_LBUTTONUP, POINT{}, SIZE{}, false) != trayNoAction {
		t.Fatal("an unmatched legacy mouse-up must not activate the panel")
	}
	if input.action(NIN_KEYSELECT, POINT{}, SIZE{}, true) != trayDetails {
		t.Fatal("keyboard activation must remain available")
	}
	if input.action(WM_RBUTTONUP, POINT{}, SIZE{}, true) != trayNoAction {
		t.Fatal("modern right-click must wait for WM_CONTEXTMENU")
	}
	if input.action(WM_CONTEXTMENU, POINT{}, SIZE{}, true) != trayMenu {
		t.Fatal("modern context menu event was ignored")
	}
	if input.action(WM_RBUTTONUP, POINT{}, SIZE{}, false) != trayMenu {
		t.Fatal("legacy context menu event was ignored")
	}
}

func TestTrayAnchorOnNegativeMonitorCoordinates(t *testing.T) {
	for _, p := range []POINT{{X: -1800, Y: -300}, {X: 1700, Y: 1050}} {
		if got := unpackTrayPoint(packTrayPoint(p)); got != p {
			t.Fatalf("anchor=%v, want %v", got, p)
		}
	}
}
