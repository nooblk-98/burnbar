// genres draws the app icon (assets/icon.png) and writes cmd/claude-usage/rsrc_windows_amd64.syso
// (icon, manifest, version info), which `go build` links into the exe automatically.
// Run it from the repository root: go run ./tools/genres 0.2.0
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"strings"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

func main() {
	ver := "0.0.0"
	if len(os.Args) > 1 {
		ver = os.Args[1]
	}
	parts := strings.Split(ver+".0.0.0", ".")[:4]
	ver4 := strings.Join(parts, ".")
	var v [4]uint16
	for i, p := range parts {
		fmt.Sscan(p, &v[i])
	}

	img := drawIcon(256)
	os.MkdirAll("assets", 0o755)
	if f, err := os.Create("assets/icon.png"); err == nil {
		png.Encode(f, img)
		f.Close()
	}

	rs := winres.ResourceSet{}
	icon, err := winres.NewIconFromResizedImage(img, nil)
	if err != nil {
		log.Fatal(err)
	}
	if err := rs.SetIcon(winres.Name("APP"), icon); err != nil {
		log.Fatal(err)
	}
	rs.SetManifest(winres.AppManifest{
		Identity:            winres.AssemblyIdentity{Name: "ClaudeUsageMeter", Version: v},
		Description:         "Claude usage meter for the Windows taskbar",
		Compatibility:       winres.Win10AndAbove,
		ExecutionLevel:      winres.AsInvoker,
		DPIAwareness:        winres.DPIPerMonitorV2,
		UseCommonControlsV6: true,
		LongPathAware:       true,
	})
	vi := version.Info{}
	vi.SetFileVersion(ver4)
	vi.SetProductVersion(ver4)
	for k, val := range map[string]string{
		version.ProductName:      "Claude Usage Meter",
		version.FileDescription:  "Claude Usage Meter",
		version.InternalName:     "claude-usage",
		version.OriginalFilename: "claude-usage.exe",
		version.FileVersion:      ver,
		version.ProductVersion:   ver,
	} {
		vi.Set(version.LangDefault, k, val)
	}
	rs.SetVersionInfo(vi)

	f, err := os.Create("cmd/claude-usage/rsrc_windows_amd64.syso")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := rs.WriteObject(f, winres.ArchAMD64); err != nil {
		log.Fatal(err)
	}
}

// drawIcon renders a dark rounded tile with the orange (5h) and green (7d) bars.
func drawIcon(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	s := float64(size) / 256
	roundRect(img, 8*s, 8*s, 248*s, 248*s, 52*s, color.NRGBA{0x1F, 0x1F, 0x1F, 0xFF})
	track := color.NRGBA{0xFF, 0xFF, 0xFF, 0x38}
	roundRect(img, 40*s, 72*s, 216*s, 116*s, 22*s, track)
	roundRect(img, 40*s, 72*s, 152*s, 116*s, 22*s, color.NRGBA{0xE8, 0x74, 0x3B, 0xFF})
	roundRect(img, 40*s, 140*s, 216*s, 184*s, 22*s, track)
	roundRect(img, 40*s, 140*s, 104*s, 184*s, 22*s, color.NRGBA{0x3F, 0xB9, 0x7A, 0xFF})
	return img
}

func roundRect(img *image.NRGBA, x0, y0, x1, y1, r float64, c color.NRGBA) {
	hw, hh := (x1-x0)/2, (y1-y0)/2
	mx, my := x0+hw, y0+hh
	for y := int(y0); y < int(math.Ceil(y1)); y++ {
		for x := int(x0); x < int(math.Ceil(x1)); x++ {
			qx := math.Abs(float64(x)+0.5-mx) - (hw - r)
			qy := math.Abs(float64(y)+0.5-my) - (hh - r)
			d := math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - r
			cov := math.Max(0, math.Min(1, 0.5-d))
			if cov == 0 {
				continue
			}
			// straight-alpha "over" blend
			dst := img.NRGBAAt(x, y)
			sa := float64(c.A) / 255 * cov
			da := float64(dst.A) / 255
			oa := sa + da*(1-sa)
			mix := func(sc, dc uint8) uint8 {
				return uint8((float64(sc)*sa + float64(dc)*da*(1-sa)) / oa)
			}
			img.SetNRGBA(x, y, color.NRGBA{mix(c.R, dst.R), mix(c.G, dst.G), mix(c.B, dst.B), uint8(oa * 255)})
		}
	}
}
