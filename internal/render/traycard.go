package render

import (
	"fmt"
	"image"
	"image/draw"
	"math"
	"time"

	"github.com/nooblk-98/burnbar/internal/usage"
	xdraw "golang.org/x/image/draw"
)

// TrayCard is the small persistent overview; the full charts remain a click away.
func TrayCard(scale float64, readings []usage.Usage, th Theme) *image.RGBA {
	return trayCard(scale, readings, th, "")
}

func TrayMilestoneCard(scale float64, readings []usage.Usage, th Theme, notice string) *image.RGBA {
	return trayCard(scale, readings, th, notice)
}

func trayCard(scale float64, readings []usage.Usage, th Theme, notice string) *image.RGBA {
	noticeFace := faceFor(11*scale, true)
	lines := wrapText(noticeFace, notice, 292*scale)
	if len(lines) > 2 {
		lines = lines[:2]
	}
	header := float64(len(lines) * 16)
	w := int(math.Round(320 * scale))
	h := int(math.Round((float64(30+len(readings)*94) + header) * scale))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fillRoundRect(img, 0, 0, float64(w), float64(h), 10*scale, th.border)
	fillRoundRect(img, 1, 1, float64(w)-1, float64(h)-1, 10*scale-1, th.bg)
	title, body, small := faceFor(12*scale, true), faceFor(11*scale, true), faceFor(10*scale, false)
	for i, line := range lines {
		drawText(img, noticeFace, line, 14*scale, float64(18+i*16)*scale, th.text)
	}
	for i, u := range readings {
		y := (float64(20+i*94) + header) * scale
		drawText(img, title, u.Name(), 14*scale, y, th.text)
		if u.Err != "" {
			status := "Unavailable"
			if len(u.All()) > 0 {
				status = "Last reading"
			}
			drawTextRight(img, small, status, float64(w)-14*scale, y, colorHot)
		}
		for j, win := range []*usage.Window{u.FiveHour, u.SevenDay} {
			cy := y + float64(20+j*26)*scale
			label := usage.WindowShortLabel(win, []string{"1st", "2nd"}[j])
			drawText(img, body, label, 14*scale, cy, th.sub)
			value := "–"
			if win != nil {
				value = fmt.Sprintf("%.0f%%", win.Percent)
			}
			drawTextRight(img, body, value, float64(w)-14*scale, cy, barColor(win))
			left := max(48*scale, 18*scale+textWidth(body, label))
			drawMeter(img, left, cy-7*scale, float64(w)-60*scale, cy-2*scale, win, th, scale)
			if win != nil && !win.ResetsAt.IsZero() {
				drawText(img, small, "Resets in "+shortDuration(time.Until(win.ResetsAt)), left, cy+11*scale, th.sub)
			}
		}
	}
	footer := "Click for details · right-click for options"
	if notice != "" {
		footer = "Auto closes · click to keep open"
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
