package render

import (
	"fmt"
	"image"
	"image/draw"
	"math"
	"time"

	"github.com/nooblk-98/burnbar/internal/alerts"
	"github.com/nooblk-98/burnbar/internal/insights"
	"github.com/nooblk-98/burnbar/internal/usage"
	xdraw "golang.org/x/image/draw"
)

// TrayCard is the small persistent overview; the full charts remain a click away.
func TrayCard(scale float64, readings []usage.Usage, th Theme) *image.RGBA {
	return trayCard(scale, readings, th, "", false, 10*time.Minute)
}

func TrayMilestoneCard(scale float64, readings []usage.Usage, th Theme, notice string) *image.RGBA {
	return trayCard(scale, readings, th, notice, false, 10*time.Minute)
}

func TrayPinnedCard(scale float64, readings []usage.Usage, th Theme) *image.RGBA {
	return trayCard(scale, readings, th, "", true, 10*time.Minute)
}

func trayCard(scale float64, readings []usage.Usage, th Theme, notice string, persistent bool, staleAfter time.Duration) *image.RGBA {
	noticeFace := faceFor(11*scale, true)
	lines := wrapText(noticeFace, notice, 292*scale)
	if len(lines) > 2 {
		lines = lines[:2]
	}
	header := float64(len(lines) * 16)
	w := int(math.Round(320 * scale))
	height := 30.
	for _, u := range readings {
		if len(u.All()) == 0 {
			height += 76
		} else {
			height += 114
		}
	}
	h := int(math.Round((height + header) * scale))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fillRoundRect(img, 0, 0, float64(w), float64(h), 10*scale, th.border)
	fillRoundRect(img, 1, 1, float64(w)-1, float64(h)-1, 10*scale-1, th.bg)
	title, body, small := faceFor(12*scale, true), faceFor(11*scale, true), faceFor(10*scale, false)
	for i, line := range lines {
		drawText(img, noticeFace, line, 14*scale, float64(18+i*16)*scale, th.text)
	}
	offset := 20.
	for _, u := range readings {
		y := (offset + header) * scale
		hasData := len(u.All()) > 0
		rowHeight := 114.
		if !hasData {
			rowHeight = 76
		}
		offset += rowHeight
		drawText(img, title, u.Name(), 14*scale, y, th.text)
		status := insights.Health(u, time.Now(), staleAfter)
		if status != "" {
			label := "! Last reading"
			if len(u.All()) == 0 {
				label = "! Unavailable"
			}
			drawTextRight(img, small, label, float64(w)-14*scale, y, th.warning())
		}
		statusLine := insights.Freshness(u, time.Now())
		if status != "" {
			statusLine = status + " · " + statusLine
		}
		statusOffset := 74
		if !hasData {
			statusOffset = 24
		}
		for k, line := range wrapText(small, statusLine, 292*scale) {
			if k >= 2 {
				break
			}
			drawText(img, small, line, 14*scale, y+float64(statusOffset+k*13)*scale, th.sub)
		}
		if !hasData {
			continue
		}
		for j, win := range []*usage.Window{u.FiveHour, u.SevenDay} {
			cy := y + float64(20+j*26)*scale
			label := usage.WindowShortLabel(win, []string{"Session", "Weekly"}[j])
			drawText(img, body, label, 14*scale, cy, th.sub)
			value := "–"
			if win != nil {
				value = fmt.Sprintf("%.0f%%", win.Percent)
			}
			drawTextRight(img, body, value, float64(w)-14*scale, cy, th.bar(win))
			left := max(48*scale, 18*scale+textWidth(body, label))
			drawMeter(img, left, cy-7*scale, float64(w)-60*scale, cy-2*scale, win, th, scale)
			if win != nil && !win.ResetsAt.IsZero() {
				drawText(img, small, "Resets in "+shortDuration(time.Until(win.ResetsAt)), left, cy+11*scale, th.sub)
			}
		}
	}
	footer := "Click / Enter: details · F6: readable usage"
	if persistent {
		footer = "Always shown · click for details"
	}
	drawText(img, small, footer, 14*scale, float64(h)-11*scale, th.sub)
	return img
}

// TrayBubble adds a restrained pointer and shadow to the rounded card.
func TrayBubble(content *image.RGBA, scale float64, th Theme, tipX int, tailTop bool) *image.RGBA {
	pad, tail := max(2, int(4*scale)), max(4, int(8*scale))
	w, h := content.Rect.Dx()+2*pad, content.Rect.Dy()+2*pad+tail
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	y := pad
	if tailTop {
		y += tail
	}
	fillRoundRect(img, 1, float64(y+2), float64(w-1), float64(y+content.Rect.Dy()+pad), 12*scale, withAlpha(th.text, 24))
	draw.Draw(img, content.Rect.Add(image.Pt(pad, y)), content, content.Rect.Min, draw.Over)
	cx := max(pad+tail+int(10*scale), min(tipX, w-pad-tail-int(10*scale)))
	for dy := 0; dy < tail; dy++ {
		py := y + content.Rect.Dy() - 1 + dy
		if tailTop {
			py = y - dy
		}
		half := tail - dy
		for x := cx - half; x <= cx+half; x++ {
			c := th.bg
			if x == cx-half || x == cx+half || dy == tail-1 {
				c = th.border
			}
			blendPixel(img, x, py, c, 1)
		}
	}
	return img
}

// FitPopup constrains the complete image before choosing screen coordinates.
func FitPopup(img *image.RGBA, maxW, maxH int) *image.RGBA {
	if img.Rect.Dx() <= maxW && img.Rect.Dy() <= maxH {
		return img
	}
	ratio := min(float64(maxW)/float64(img.Rect.Dx()), float64(maxH)/float64(img.Rect.Dy()))
	w, h := max(1, int(float64(img.Rect.Dx())*ratio)), max(1, int(float64(img.Rect.Dy())*ratio))
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(out, out.Rect, img, img.Rect, draw.Src, nil)
	return out
}

// TrayStatusCard displays freshness with the configured refresh interval.
func TrayStatusCard(scale float64, readings []usage.Usage, th Theme, staleAfter time.Duration) *image.RGBA {
	return trayCard(scale, readings, th, "", false, staleAfter)
}

// CompactNotice deliberately contains only the quotas that just crossed a
// threshold. Several quotas share one timed notice, never a cascade of cards.
func CompactNotice(scale float64, events []alerts.Milestone, th Theme) *image.RGBA {
	title, body, small := faceFor(12*scale, true), faceFor(11*scale, false), faceFor(10*scale, false)
	var lines []string
	for _, e := range events {
		text := fmt.Sprintf("%s %s: %.0f%% used (%d%% crossed)", e.Provider.Name(), e.Label, e.Percent, e.Level)
		lines = append(lines, wrapText(body, text, 292*scale)...)
		if !e.ResetsAt.IsZero() {
			lines = append(lines, wrapText(body, "Resets in "+shortDuration(time.Until(e.ResetsAt)), 292*scale)...)
		}
	}
	h := int(float64(66+len(lines)*17) * scale)
	w := int(320 * scale)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fillRoundRect(img, 0, 0, float64(w), float64(h), 10*scale, th.border)
	fillRoundRect(img, 1, 1, float64(w)-1, float64(h)-1, 10*scale-1, th.bg)
	drawText(img, title, "Usage milestone", 14*scale, 22*scale, th.text)
	for i, line := range lines {
		drawText(img, body, line, 14*scale, float64(44+i*17)*scale, th.text)
	}
	drawText(img, small, "Click for full usage · closes automatically", 14*scale, float64(h)-12*scale, th.sub)
	return img
}

func TrayPinnedStatusCard(scale float64, readings []usage.Usage, th Theme, staleAfter time.Duration) *image.RGBA {
	return trayCard(scale, readings, th, "", true, staleAfter)
}
