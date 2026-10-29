package render

import (
	"strings"
	"testing"

	"github.com/nooblk-98/burnbar/internal/usage"
)

func TestProviderRendering(t *testing.T) {
	readings := []usage.Usage{usage.DemoProvider(usage.Claude), usage.DemoProvider(usage.Codex)}
	for _, scale := range []float64{1, 1.25, 2} {
		w, h := GroupWidgetSize(scale, 2)
		img := GroupWidget(scale, readings, NewTheme(false), false)
		if img.Rect.Dx() != w || img.Rect.Dy() != h {
			t.Fatalf("widget size: %v", img.Rect)
		}
		panels := []Panel{{Usage: readings[0]}, {Usage: readings[1]}}
		for _, width := range []int{int(1400 * scale), int(400 * scale)} {
			popup := GroupPopup(scale, panels, NewTheme(false), width, int(900*scale))
			if popup.Rect.Dx() > width || popup.Rect.Dy() > int(900*scale) {
				t.Fatalf("popup exceeds work area: %v", popup.Rect)
			}
		}
	}
	tip := GroupTooltip(readings)
	if !strings.Contains(tip, "Claude: 5h 62%") || !strings.Contains(tip, "Codex: 5h 38%") {
		t.Fatalf("tooltip: %s", tip)
	}
	readings[1] = usage.Usage{Provider: usage.Codex, Err: "not installed"}
	if !strings.Contains(GroupTooltip(readings), "Codex: unavailable") {
		t.Fatal("Codex error must not hide Claude")
	}
}

func TestNarrowMeterAndFloatingSurface(t *testing.T) {
	u := usage.DemoProvider(usage.Claude)
	for _, scale := range []float64{1, 1.25, 2} {
		width := MinimumWidgetWidth(scale)
		meter := ProviderWidgetSized(scale, u, NewTheme(false), false, width)
		if meter.Rect.Dx() != width {
			t.Fatalf("unexpected narrow width: %v", meter.Rect)
		}
		floating := FloatingWidget(meter, NewTheme(false), scale)
		if floating.Rect != meter.Rect || floating.RGBAAt(width/2, meter.Rect.Dy()/2).A != 255 {
			t.Fatal("floating fallback must keep its footprint and have an opaque surface")
		}
	}
}
