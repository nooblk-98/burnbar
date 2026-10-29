// Package layout decides where on the taskbar the meter fits.
package layout

import (
	"sort"
	"strings"
)

// Rect matches the Win32 RECT layout, so the Windows code can use it directly.
type Rect struct{ Left, Top, Right, Bottom int32 }

// Item is one element of the taskbar's UI Automation tree.
type Item struct {
	Class       string
	Rect        Rect
	ControlType int32
}

// Analyze reduces the taskbar's UI Automation tree to the spans its buttons occupy and
// the left edge of the notification area (tray icons, clock).
func Analyze(items []Item) (occupied []Rect, trayLeft int, ok bool) {
	trayLeft = -1
	for _, it := range items {
		switch {
		case strings.HasPrefix(it.Class, "SystemTray."):
			if trayLeft < 0 || int(it.Rect.Left) < trayLeft {
				trayLeft = int(it.Rect.Left)
			}
		case it.Class == "Taskbar.TaskListButtonAutomationPeer", it.Class == "ToggleButton",
			it.Class == "Button", it.Class == "SearchBox", it.ControlType == 50000, it.ControlType == 50002,
			it.ControlType == 50003, it.ControlType == 50004, it.ControlType == 50031:
			occupied = append(occupied, it.Rect)
		}
	}
	return occupied, trayLeft, trayLeft >= 0
}

type span struct{ l, r int }

// PickSlot chooses an x for a widget of width w between left and right that avoids the occupied
// rectangles. mode "right" prefers the notification area; "left" prefers the leftmost free
// gap; "auto" prefers the space next to the notification area, then any other gap that fits.
// When nothing fits, ok is false: callers must use the tray rather than cover buttons.
func PickSlot(occupied []Rect, left, right, w, margin int, mode string) (x int, ok bool) {
	if w <= 0 || right <= left || margin < 0 {
		return 0, false
	}
	occupied = append([]Rect(nil), occupied...)
	sort.Slice(occupied, func(i, j int) bool { return occupied[i].Left < occupied[j].Left })
	var gaps []span
	cur := left
	for _, r := range occupied {
		l, rr := max(int(r.Left), left), min(int(r.Right), right)
		if rr <= l {
			continue
		}
		if l > cur {
			gaps = append(gaps, span{cur, l})
		}
		cur = max(cur, rr)
	}
	if right > cur {
		gaps = append(gaps, span{cur, right})
	}
	need := w + 2*margin
	fits := func(g span) bool { return g.r-g.l >= need }

	if mode == "left" {
		for _, g := range gaps {
			if fits(g) {
				return g.l + margin, true
			}
		}
	} else {
		if n := len(gaps); n > 0 && gaps[n-1].r == right && fits(gaps[n-1]) {
			return right - margin - w, true
		}
		for i := len(gaps) - 1; i >= 0; i-- {
			if fits(gaps[i]) {
				return gaps[i].l + margin, true
			}
		}
	}
	return 0, false
}
