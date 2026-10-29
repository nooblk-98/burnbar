package render

import (
	"fmt"
	"image"
	"image/draw"
	"math"
	"strings"
	"time"

	"github.com/nooblk-98/burnbar/internal/history"
	"github.com/nooblk-98/burnbar/internal/usage"
	"golang.org/x/image/font"
)

type Panel struct {
	Usage   usage.Usage
	History []history.Sample
}

func wrapText(face font.Face, text string, width float64) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		if line != "" {
			if textWidth(face, line+" "+word) > width {
				lines = append(lines, line)
				line = ""
			} else {
				line += " "
			}
		}
		// Paths and server bucket names can be longer than a line.
		for _, r := range word {
			if line != "" && textWidth(face, line+string(r)) > width {
				lines = append(lines, line)
				line = ""
			}
			line += string(r)
		}
	}
	if strings.TrimSpace(line) != "" {
		lines = append(lines, strings.TrimSpace(line))
	}
	return lines
}

func GroupWidgetSize(scale float64, count int) (int, int) {
	return WidgetSize(scale)
}

// GroupWidget renders the first provider; the app rotates the active provider.
func GroupWidget(scale float64, readings []usage.Usage, th Theme, hover bool) *image.RGBA {
	if len(readings) > 0 {
		return ProviderWidget(scale, readings[0], th, hover)
	}
	return ProviderWidget(scale, usage.Usage{}, th, hover)
}

// ProviderWidget fits a provider caption and two quota rows into the original footprint.
func ProviderWidget(scale float64, u usage.Usage, th Theme, hover bool) *image.RGBA {
	w, _ := WidgetSize(scale)
	return ProviderWidgetSized(scale, u, th, hover, w)
}

func MinimumWidgetWidth(scale float64) int { return int(math.Ceil(96 * scale)) }

// ProviderWidgetSized keeps text readable while shortening the meter tracks.
func ProviderWidgetSized(scale float64, u usage.Usage, th Theme, hover bool, width int) *image.RGBA {
	_, h := WidgetSize(scale)
	w := max(width, MinimumWidgetWidth(scale))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 1
	}
	caption := faceFor(8.5*scale, true)
	drawText(img, caption, strings.ToUpper(u.Name()), scale, 8*scale, th.sub)
	face := faceFor(10.5*scale, true)
	for i, win := range []*usage.Window{u.FiveHour, u.SevenDay} {
		cy := (18 + float64(i)*13) * scale
		baseline := cy + capHeight(face)/2
		fallback := []string{"5h", "7d"}[i]
		if u.Provider == usage.Codex {
			fallback = []string{"1st", "2nd"}[i]
		}
		label := usage.WindowShortLabel(win, fallback)
		drawText(img, face, label, scale, baseline, th.sub)
		bx := math.Max(23*scale, textWidth(face, label)+3*scale)
		drawMeter(img, bx, cy-2.5*scale, float64(w)-42*scale, cy+2.5*scale, win, th, scale)
		value := "–"
		if u.Err != "" && win == nil {
			value = "!"
		}
		if win != nil {
			value = fmt.Sprintf("%.0f%%", win.Percent)
			if hover && !win.ResetsAt.IsZero() {
				value = shortDuration(time.Until(win.ResetsAt))
			}
		}
		drawTextRight(img, face, value, float64(w)-scale, baseline, barColor(win))
	}
	return img
}

// FloatingWidget gives the fallback above a crowded taskbar an opaque surface.
func FloatingWidget(meter *image.RGBA, th Theme, scale float64) *image.RGBA {
	img := image.NewRGBA(meter.Rect)
	w, h := float64(img.Rect.Dx()), float64(img.Rect.Dy())
	fillRoundRect(img, 0, 0, w, h, 4*scale, th.border)
	fillRoundRect(img, 1, 1, w-1, h-1, 4*scale-1, th.bg)
	draw.Draw(img, img.Rect, meter, image.Point{}, draw.Over)
	return img
}

// GroupPopup displays independent provider panels. If a full view cannot fit the
// monitor, summaries retain the main limits and errors while omitting charts.
func GroupPopup(scale float64, panels []Panel, th Theme, maxW, maxH int) *image.RGBA {
	renderPanels := func(compact bool) []*image.RGBA {
		var images []*image.RGBA
		for _, panel := range panels {
			u, hist := panel.Usage, panel.History
			if compact {
				u.Extra, u.Breakdown, hist = nil, nil, nil
			}
			images = append(images, Popup(scale, u, hist, th))
		}
		return images
	}
	images := renderPanels(false)
	if len(images) == 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	gap := int(math.Round(8 * scale))
	sideBySide := len(images)*images[0].Rect.Dx()+(len(images)-1)*gap <= maxW
	dimensions := func() (int, int) {
		w, h := 0, 0
		for _, img := range images {
			if sideBySide {
				w += img.Rect.Dx() + gap
				h = max(h, img.Rect.Dy())
			} else {
				w = max(w, img.Rect.Dx())
				h += img.Rect.Dy() + gap
			}
		}
		if sideBySide {
			w -= gap
		} else {
			h -= gap
		}
		return w, h
	}
	w, h := dimensions()
	if h > maxH {
		images = renderPanels(true)
		w, h = dimensions()
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	x, y := 0, 0
	for _, panel := range images {
		draw.Draw(img, panel.Rect.Add(image.Pt(x, y)), panel, image.Point{}, draw.Over)
		if sideBySide {
			x += panel.Rect.Dx() + gap
		} else {
			y += panel.Rect.Dy() + gap
		}
	}
	return img
}

func GroupTrayIcon(size int, readings []usage.Usage, light bool) *image.RGBA {
	if len(readings) == 1 {
		return TrayIcon(size, readings[0], light)
	}
	// Each icon row shows the more-used quota for that provider.
	var windows []*usage.Window
	for _, u := range readings {
		var highest *usage.Window
		for _, w := range []*usage.Window{u.FiveHour, u.SevenDay} {
			if w != nil && (highest == nil || w.Percent > highest.Percent) {
				highest = w
			}
		}
		windows = append(windows, highest)
	}
	if len(windows) < 2 {
		return TrayIcon(size, usage.Usage{}, light)
	}
	return TrayIcon(size, usage.Usage{FiveHour: windows[0], SevenDay: windows[1]}, light)
}

func GroupTooltip(readings []usage.Usage) string {
	if len(readings) == 1 {
		return Tooltip(readings[0])
	}
	var lines []string
	for _, u := range readings {
		if u.Err != "" {
			lines = append(lines, u.Name()+": unavailable (see details)")
			continue
		}
		var values []string
		for _, w := range []*usage.Window{u.FiveHour, u.SevenDay} {
			if w != nil {
				values = append(values, fmt.Sprintf("%s %.0f%%", usage.WindowShortLabel(w, "quota"), w.Percent))
			}
		}
		lines = append(lines, u.Name()+": "+strings.Join(values, " / "))
	}
	return strings.Join(lines, "\n")
}
