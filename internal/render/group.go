package render

import (
	"fmt"
	"image"
	"image/draw"
	"math"
	"strings"

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

// PopupAwareTooltip avoids a redundant native tooltip covering a visible card.
func PopupAwareTooltip(readings []usage.Usage, popupVisible bool) string {
	if popupVisible {
		return ""
	}
	return GroupTooltip(readings)
}
