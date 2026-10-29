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
	Class string
	Rect  Rect
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
		case it.Class == "Taskbar.TaskListButtonAutomationPeer", it.Class == "ToggleButton":
			occupied = append(occupied, it.Rect)
		}
	}
	return occupied, trayLeft, trayLeft >= 0
}

type span struct{ l, r int }

// PickSlot chooses an x for a widget of width w between left and right that avoids the occupied
// rectangles. mode "right" always hugs the notification area; "left" prefers the leftmost free
// gap; "auto" prefers the space next to the notification area, then any other gap that fits.
// When nothing fits, the widget is centred in the widest gap.
func PickSlot(occupied []Rect, left, right, w, margin int, mode string) int {
	if mode == "right" {
		return right - margin - w
	}
	sort.Slice(occupied, func(i, j int) bool { return occupied[i].Left < occupied[j].Left })
	var gaps []span
	cur := left
	for _, r := range occupied {
		l, rr := max(int(r.Left), left), min(int(r.Right), right)
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
				return g.l + margin
			}
		}
	} else {
		if n := len(gaps); n > 0 && gaps[n-1].r == right && fits(gaps[n-1]) {
			return right - margin - w
		}
		for i := len(gaps) - 1; i >= 0; i-- {
			if fits(gaps[i]) {
				return gaps[i].l + margin
			}
		}
	}
	if len(gaps) == 0 {
		return right - margin - w
	}
	best := gaps[0]
	for _, g := range gaps {
		if g.r-g.l > best.r-best.l {
			best = g
		}
	}
	return best.l + (best.r-best.l-w)/2
}
