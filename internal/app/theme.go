package app

import (
	"github.com/nooblk-98/burnbar/internal/render"
	"image/color"
	"unsafe"
)

func sysColor(index uintptr) color.NRGBA {
	c, _, _ := pGetSysColor.Call(index)
	return color.NRGBA{R: uint8(c), G: uint8(c >> 8), B: uint8(c >> 16), A: 255}
}
func (a *app) theme() render.Theme {
	hc := struct {
		Size, Flags uint32
		Scheme      uintptr
	}{}
	hc.Size = uint32(unsafe.Sizeof(hc))
	r, _, _ := pSystemParametersInfoW.Call(0x42, uintptr(hc.Size), uintptr(unsafe.Pointer(&hc)), 0)
	if r != 0 && hc.Flags&1 != 0 {
		return render.HighContrastTheme(sysColor(5), sysColor(8), sysColor(13))
	} // WINDOW / WINDOWTEXT / HIGHLIGHT
	light := systemUsesLightTheme()
	if a.cfg.Theme == "light" {
		light = true
	}
	if a.cfg.Theme == "dark" {
		light = false
	}
	return render.NewTheme(light)
}
func (a *app) renderScale() float64 {
	return float64(dpiForWindow(a.popupTB)) / 96 * float64(a.cfg.TextScale) / 100
}
