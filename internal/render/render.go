package render

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nooblk-98/burnbar/internal/history"
	"github.com/nooblk-98/burnbar/internal/usage"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

var (
	color5h    = color.NRGBA{0xE8, 0x74, 0x3B, 0xFF} // orange
	color7d    = color.NRGBA{0x3F, 0xB9, 0x7A, 0xFF} // green
	colorOther = color.NRGBA{0x5B, 0x8D, 0xEF, 0xFF} // blue, for any extra limits
	colorWarn  = color.NRGBA{0xE0, 0x9B, 0x22, 0xFF} // amber, "above pace"
	colorHot   = color.NRGBA{0xE5, 0x48, 0x4D, 0xFF} // red, used at >= 90%
	colorOff   = color.NRGBA{0x8A, 0x8A, 0x8A, 0xFF}
)

type Theme struct {
	light  bool
	text   color.NRGBA
	sub    color.NRGBA
	track  color.NRGBA
	bg     color.NRGBA
	border color.NRGBA
}

func NewTheme(light bool) Theme {
	if light {
		return Theme{light: true,
			text: color.NRGBA{0x1A, 0x1A, 0x1A, 0xFF}, sub: color.NRGBA{0x5F, 0x5F, 0x5F, 0xFF},
			track: color.NRGBA{0, 0, 0, 0x26}, bg: color.NRGBA{0xF9, 0xF9, 0xF9, 0xFF},
			border: color.NRGBA{0xD6, 0xD6, 0xD6, 0xFF}}
	}
	return Theme{
		text: color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}, sub: color.NRGBA{0xA8, 0xA8, 0xA8, 0xFF},
		track: color.NRGBA{0xFF, 0xFF, 0xFF, 0x30}, bg: color.NRGBA{0x2B, 0x2B, 0x2B, 0xFF},
		border: color.NRGBA{0x45, 0x45, 0x45, 0xFF}}
}

func withAlpha(c color.NRGBA, a uint8) color.NRGBA { c.A = a; return c }

// Fonts

var (
	fontOnce  sync.Once
	fontFiles = map[bool]*opentype.Font{} // keyed by "semibold"
	faceCache = map[string]font.Face{}
)

func faceFor(px float64, semibold bool) font.Face {
	fontOnce.Do(func() {
		dir := filepath.Join(os.Getenv("WINDIR"), "Fonts")
		for bold, names := range map[bool][]string{true: {"seguisb.ttf", "segoeui.ttf"}, false: {"segoeui.ttf"}} {
			for _, name := range names {
				if b, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
					if f, err := opentype.Parse(b); err == nil {
						fontFiles[bold] = f
						break
					}
				}
			}
		}
	})
	f := fontFiles[semibold]
	if f == nil {
		return basicfont.Face7x13
	}
	px = math.Round(px*2) / 2
	key := fmt.Sprint(px, semibold)
	if face, ok := faceCache[key]; ok {
		return face
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: px, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return basicfont.Face7x13
	}
	faceCache[key] = face
	return face
}

// Drawing primitives on a premultiplied image.RGBA

func blendPixel(img *image.RGBA, x, y int, c color.NRGBA, cov float64) {
	if cov <= 0 || !(image.Point{x, y}.In(img.Rect)) {
		return
	}
	a := float64(c.A) / 255 * math.Min(cov, 1)
	i := img.PixOffset(x, y)
	p := img.Pix[i : i+4 : i+4]
	inv := 1 - a
	p[0] = uint8(float64(c.R)*a + float64(p[0])*inv + 0.5)
	p[1] = uint8(float64(c.G)*a + float64(p[1])*inv + 0.5)
	p[2] = uint8(float64(c.B)*a + float64(p[2])*inv + 0.5)
	p[3] = uint8(255*a + float64(p[3])*inv + 0.5)
}

// fillRoundRect draws an anti-aliased rounded rectangle using a signed distance field.
func fillRoundRect(img *image.RGBA, x0, y0, x1, y1, r float64, c color.NRGBA) {
	if x1 <= x0 || y1 <= y0 {
		return
	}
	hw, hh := (x1-x0)/2, (y1-y0)/2
	r = math.Min(r, math.Min(hw, hh))
	mx, my := x0+hw, y0+hh
	for y := int(math.Floor(y0)); y < int(math.Ceil(y1)); y++ {
		for x := int(math.Floor(x0)); x < int(math.Ceil(x1)); x++ {
			qx := math.Abs(float64(x)+0.5-mx) - (hw - r)
			qy := math.Abs(float64(y)+0.5-my) - (hh - r)
			d := math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - r
			blendPixel(img, x, y, c, 0.5-d)
		}
	}
}

func drawText(img *image.RGBA, face font.Face, s string, x, baseline float64, c color.NRGBA) {
	d := font.Drawer{Dst: img, Src: image.NewUniform(c), Face: face, Dot: fixed.P(int(math.Round(x)), int(math.Round(baseline)))}
	d.DrawString(s)
}

func drawTextRight(img *image.RGBA, face font.Face, s string, right, baseline float64, c color.NRGBA) {
	drawText(img, face, s, right-textWidth(face, s), baseline, c)
}

func textWidth(face font.Face, s string) float64 {
	return float64(font.MeasureString(face, s)) / 64
}

func capHeight(face font.Face) float64 {
	return float64(face.Metrics().Ascent) / 64 * 0.72
}

func baseColor(w *usage.Window) color.NRGBA {
	switch {
	case w == nil:
		return colorOff
	case w.Key == "session" || w.Key == "primary":
		return color5h
	case w.Key == "weekly_all" || w.Key == "secondary":
		return color7d
	}
	return colorOther
}

func barColor(w *usage.Window) color.NRGBA {
	if w != nil && w.Percent >= 90 {
		return colorHot
	}
	return baseColor(w)
}

// drawMeter draws a pill-shaped bar with the used fraction filled and, when known, a pace tick
// marking how much of the window's time has passed.
func drawMeter(img *image.RGBA, x0, y0, x1, y1 float64, w *usage.Window, th Theme, scale float64) {
	h := y1 - y0
	fillRoundRect(img, x0, y0, x1, y1, h/2, th.track)
	if w == nil {
		return
	}
	if frac := math.Max(0, math.Min(1, w.Percent/100)); frac > 0 {
		fillRoundRect(img, x0, y0, x0+math.Max(frac*(x1-x0), h), y1, h/2, barColor(w))
	}
	if e, ok := w.Elapsed(); ok {
		tx := x0 + e*(x1-x0)
		tw := math.Max(1.5*scale, 1.5)
		fillRoundRect(img, tx-tw/2, y0-2*scale, tx+tw/2, y1+2*scale, tw/2, withAlpha(th.text, 0xD0))
	}
}

func shortDuration(d time.Duration) string {
	if d <= 0 {
		return "now"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	switch {
	case h >= 24:
		return fmt.Sprintf("%dd%dh", h/24, h%24)
	case h > 0:
		return fmt.Sprintf("%dh%02dm", h, m)
	default:
		return fmt.Sprintf("%dm", m)
	}
}

func ResetText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	layout := "3:04 PM"
	if time.Until(t) > 20*time.Hour {
		layout = "Mon 3:04 PM"
	}
	return fmt.Sprintf("Resets in %s (%s)", shortDuration(time.Until(t)), t.Local().Format(layout))
}

// paceText compares usage with elapsed time: above pace means the limit will run out before it resets.
func paceText(w *usage.Window) (string, bool) {
	e, ok := w.Elapsed()
	if !ok {
		return "", false
	}
	if w.Percent > e*100+5 {
		return "Above pace", true
	}
	return "Within pace", false
}

// Taskbar widget, in 96-DPI units.
const (
	widgetW  = 130
	widgetH  = 38
	labelW   = 18
	valueW   = 40
	gap      = 4
	barH     = 6
	fontPx   = 11.5
	rowPitch = 17
)

func WidgetSize(scale float64) (int, int) {
	return int(math.Round(widgetW * scale)), int(math.Round(widgetH * scale))
}

// Widget draws the two-row meter. When hover is set, the value column shows time-to-reset.
func Widget(scale float64, u usage.Usage, th Theme, hover bool) *image.RGBA {
	w, h := WidgetSize(scale)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// A nearly invisible backdrop keeps the whole widget clickable (alpha 0 pixels are click-through).
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 1
	}

	face := faceFor(fontPx*scale, true)
	capH := capHeight(face)
	rows := []struct {
		label string
		win   *usage.Window
	}{{usage.WindowShortLabel(u.FiveHour, "1st"), u.FiveHour}, {usage.WindowShortLabel(u.SevenDay, "2nd"), u.SevenDay}}
	for i, row := range rows {
		cy := float64(h)/2 + (float64(i)-0.5)*rowPitch*scale
		baseline := cy + capH/2

		drawText(img, face, row.label, 1*scale, baseline, th.text)
		bx0 := (labelW + gap) * scale
		bx1 := float64(w) - (valueW+gap)*scale
		drawMeter(img, bx0, cy-barH*scale/2, bx1, cy+barH*scale/2, row.win, th, scale)

		value := "–"
		if u.Err != "" && row.win == nil {
			value = "!"
		}
		if row.win != nil {
			value = fmt.Sprintf("%.0f%%", row.win.Percent)
			if hover && !row.win.ResetsAt.IsZero() {
				value = shortDuration(time.Until(row.win.ResetsAt))
			}
		}
		drawTextRight(img, face, value, float64(w)-1*scale, baseline, barColor(row.win))
	}
	return img
}

// Detail popup, in 96-DPI units.
const (
	popupW   = 320
	popupPad = 16
)

// drawSpark draws an area chart of one series of samples between from and to.
func drawSpark(img *image.RGBA, x0, y0, x1, y1 float64, samples []history.Sample, get func(history.Sample) float64,
	from, to time.Time, maxGap time.Duration, c color.NRGBA, th Theme, scale float64) {
	fillRoundRect(img, x0, y0, x1, y1, 3*scale, withAlpha(th.track, th.track.A/2))
	// 80% guide line
	gy := y1 - 0.8*(y1-y0)
	for x := x0 + 2*scale; x < x1-2*scale; x += 4 * scale {
		fillRoundRect(img, x, gy-0.5*scale, x+2*scale, gy+0.5*scale, 0, withAlpha(th.sub, 0x50))
	}
	span := to.Sub(from).Seconds()
	gapSec := maxGap.Seconds()
	j := 0
	prevTop := math.NaN()
	for px := int(x0); px < int(x1); px++ {
		t := float64(from.Unix()) + float64(px-int(x0))/(x1-x0)*span
		for j+1 < len(samples) && float64(samples[j+1].T) <= t {
			j++
		}
		if len(samples) == 0 || float64(samples[j].T) > t || t-float64(samples[j].T) > gapSec || get(samples[j]) < 0 {
			prevTop = math.NaN()
			continue
		}
		// Interpolate towards the next reading when it is close enough to belong to the same run.
		v := get(samples[j])
		if j+1 < len(samples) {
			n := samples[j+1]
			if dt := float64(n.T - samples[j].T); dt > 0 && dt <= gapSec && get(n) >= 0 {
				v += (get(n) - v) * (t - float64(samples[j].T)) / dt
			}
		}
		top := y1 - math.Min(v, 100)/100*(y1-y0)
		for py := int(math.Floor(top)); py < int(math.Ceil(y1)); py++ {
			blendPixel(img, px, py, withAlpha(c, 0x55), math.Min(1, float64(py+1)-top))
		}
		// Line: cover the vertical span from the previous column so steep changes stay connected.
		lo, hi := top, top
		if !math.IsNaN(prevTop) {
			lo, hi = math.Min(prevTop, top), math.Max(prevTop, top)
		}
		half := 0.8 * scale
		for py := int(math.Floor(lo - half)); py <= int(math.Ceil(hi+half)); py++ {
			cy := float64(py) + 0.5
			d := math.Max(lo-cy, cy-hi)
			blendPixel(img, px, py, c, math.Min(1, half+0.5-d))
		}
		prevTop = top
	}
}

func peak(samples []history.Sample, get func(history.Sample) float64) float64 {
	m := -1.0
	for _, s := range samples {
		m = math.Max(m, get(s))
	}
	return m
}

// Popup draws the details panel opened by clicking the widget or tray icon.
func Popup(scale float64, u usage.Usage, hist []history.Sample, th Theme) *image.RGBA {
	s := func(v float64) float64 { return v * scale }
	W := int(math.Round(s(popupW)))
	content := image.NewRGBA(image.Rect(0, 0, W, int(s(900))))
	title := faceFor(s(14), true)
	body := faceFor(s(12), false)
	bodyB := faceFor(s(12), true)
	small := faceFor(s(11), false)
	left, right := s(popupPad), float64(W)-s(popupPad)

	y := s(popupPad) + capHeight(title)
	drawText(content, title, u.Name()+" usage", left, y, th.text)
	if !u.UpdatedAt.IsZero() && len(u.All()) > 0 {
		drawTextRight(content, small, "Updated "+u.UpdatedAt.Format("3:04 PM"), right, y, th.sub)
	}
	y += s(10)

	for _, w := range u.All() {
		y += s(14) + capHeight(body)
		drawText(content, body, w.Label, left, y, th.text)
		drawTextRight(content, bodyB, fmt.Sprintf("%.0f%%", w.Percent), right, y, barColor(w))
		y += s(7)
		drawMeter(content, left, y, right, y+s(8), w, th, scale)
		y += s(8) + s(6) + capHeight(small)
		drawText(content, small, ResetText(w.ResetsAt), left, y, th.sub)
		if p, above := paceText(w); p != "" {
			c := th.sub
			if above {
				c = colorWarn
			}
			drawTextRight(content, small, p, right, y, c)
		}
	}

	if u.Err != "" {
		for _, line := range wrapText(small, u.Err, right-left) {
			y += s(7) + capHeight(small)
			drawText(content, small, line, left, y, colorHot)
		}
		if len(u.All()) > 0 {
			y += s(7) + capHeight(small)
			drawText(content, small, "Showing last successful reading", left, y, th.sub)
		}
	}

	now := time.Now()
	charts := []struct {
		label  string
		from   time.Time
		maxGap time.Duration
		get    func(history.Sample) float64
		c      color.NRGBA
	}{
		{"Primary – last 24 hours", now.Add(-24 * time.Hour), 20 * time.Minute, func(x history.Sample) float64 { return x.S }, color5h},
		{"Secondary – last 7 days", now.Add(-7 * 24 * time.Hour), 2 * time.Hour, func(x history.Sample) float64 { return x.W }, color7d},
	}
	for _, ch := range charts {
		var pts []history.Sample
		for _, x := range hist {
			if x.T >= ch.from.Unix() && ch.get(x) >= 0 {
				pts = append(pts, x)
			}
		}
		if len(pts) < 2 {
			continue
		}
		y += s(18) + capHeight(small)
		drawText(content, small, ch.label, left, y, th.sub)
		if p := peak(pts, ch.get); p >= 0 {
			drawTextRight(content, small, fmt.Sprintf("peak %.0f%%", p), right, y, th.sub)
		}
		y += s(6)
		drawSpark(content, left, y, right, y+s(36), pts, ch.get, ch.from, now, ch.maxGap, ch.c, th, scale)
		y += s(36)
	}

	if len(u.Breakdown) > 0 {
		y += s(18) + capHeight(small)
		drawText(content, small, "This week by product", left, y, th.sub)
		for _, sh := range u.Breakdown {
			y += s(8) + capHeight(small)
			drawText(content, small, sh.Name, left, y, th.text)
			drawTextRight(content, small, fmt.Sprintf("%.0f%%", sh.Percent), right, y, th.text)
			bx0, bx1 := left+s(96), right-s(40)
			cy := y - capHeight(small)/2
			fillRoundRect(content, bx0, cy-s(2), bx1, cy+s(2), s(2), th.track)
			fillRoundRect(content, bx0, cy-s(2), bx0+math.Max(sh.Percent/100*(bx1-bx0), s(4)), cy+s(2), s(2), colorOther)
			y += s(2)
		}
	}

	y += s(18) + capHeight(small)
	drawText(content, small, "Click to refresh · right-click for options", left, y, th.sub)
	H := int(math.Ceil(y + s(popupPad)))

	img := image.NewRGBA(image.Rect(0, 0, W, H))
	fillRoundRect(img, 0, 0, float64(W), float64(H), s(8), th.border)
	fillRoundRect(img, 1, 1, float64(W)-1, float64(H)-1, s(8)-1, th.bg)
	draw.Draw(img, img.Rect, content, image.Point{}, draw.Over)
	return img
}

// TrayIcon draws two stacked bars for the notification-area icon.
func TrayIcon(size int, u usage.Usage, light bool) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	s := float64(size) / 16
	track := color.NRGBA{0xFF, 0xFF, 0xFF, 0x50}
	if light {
		track = color.NRGBA{0, 0, 0, 0x40}
	}
	for i, w := range []*usage.Window{u.FiveHour, u.SevenDay} {
		y0 := (2 + float64(i)*7) * s
		y1 := y0 + 5*s
		fillRoundRect(img, 0, y0, float64(size), y1, 1.5*s, track)
		if w != nil && w.Percent > 0 {
			fw := math.Max(w.Percent/100*float64(size), 3*s)
			fillRoundRect(img, 0, y0, math.Min(fw, float64(size)), y1, 1.5*s, barColor(w))
		}
	}
	return img
}

func Tooltip(u usage.Usage) string {
	line := func(name string, w *usage.Window) string {
		if w == nil {
			return name + ": n/a"
		}
		s := fmt.Sprintf("%s: %.0f%%", name, w.Percent)
		if !w.ResetsAt.IsZero() {
			s += " · resets in " + shortDuration(time.Until(w.ResetsAt))
		}
		return s
	}
	lines := []string{u.Name() + " usage", line(usage.WindowShortLabel(u.FiveHour, "Primary"), u.FiveHour), line(usage.WindowShortLabel(u.SevenDay, "Secondary"), u.SevenDay)}
	if u.Err != "" {
		lines = append(lines, u.Err)
	} else if !u.UpdatedAt.IsZero() {
		lines = append(lines, "Updated "+u.UpdatedAt.Format("3:04 PM"))
	}
	return strings.Join(lines, "\n")
}

// ToBGRA converts premultiplied RGBA to the premultiplied BGRA layout GDI expects.
func ToBGRA(img *image.RGBA, dst []byte) {
	for i := 0; i+3 < len(img.Pix) && i+3 < len(dst); i += 4 {
		dst[i+0] = img.Pix[i+2]
		dst[i+1] = img.Pix[i+1]
		dst[i+2] = img.Pix[i+0]
		dst[i+3] = img.Pix[i+3]
	}
}
