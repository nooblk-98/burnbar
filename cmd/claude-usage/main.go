// Command claude-usage shows Claude's 5-hour and 7-day usage limits as two bars on the Windows taskbar.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"claude-usage/internal/app"
	"claude-usage/internal/config"
	"claude-usage/internal/history"
	"claude-usage/internal/render"
	"claude-usage/internal/usage"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	demo := flag.Bool("demo", false, "show sample numbers instead of calling the usage API")
	once := flag.Bool("once", false, "print current usage to stdout and exit")
	snapshot := flag.String("snapshot", "", "write widget and popup PNGs to this folder and exit")
	flag.Parse()
	usage.UserAgent = "claude-usage-meter/" + version

	var err error
	switch {
	case *once:
		err = printOnce()
	case *snapshot != "":
		err = writeSnapshots(*snapshot, *demo)
	default:
		err = app.Run(*demo)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func printOnce() error {
	body, err := usage.FetchRaw()
	if err != nil {
		return err
	}
	fmt.Println(string(body))
	fmt.Println()
	u := usage.Parse(body)
	fmt.Println(render.Tooltip(u))
	for _, w := range u.Extra {
		fmt.Printf("%s: %.0f%%\n", w.Label, w.Percent)
	}
	return nil
}

// writeSnapshots renders the widget and popup in both themes, for checking the design.
func writeSnapshots(dir string, demo bool) error {
	u, hist := usage.Demo(), history.Demo()
	if !demo {
		u, hist = usage.Fetch(), history.Load(config.Dir())
	}
	samples := hist.Since(time.Now().Add(-7 * 24 * time.Hour))
	for _, light := range []bool{false, true} {
		name := map[bool]string{false: "dark", true: "light"}[light]
		bgColor := map[bool]color.RGBA{false: {0x20, 0x20, 0x20, 0xFF}, true: {0xEE, 0xF0, 0xF6, 0xFF}}[light]
		th := render.NewTheme(light)
		imgs := map[string]*image.RGBA{
			"widget-" + name + ".png":       render.Widget(1.25, u, th, false),
			"widget-hover-" + name + ".png": render.Widget(1.25, u, th, true),
			"popup-" + name + ".png":        render.Popup(1.25, u, samples, th),
		}
		for file, img := range imgs {
			// Show the transparent widget on a taskbar-like background.
			bg := image.NewRGBA(img.Rect)
			draw.Draw(bg, bg.Rect, image.NewUniform(bgColor), image.Point{}, draw.Src)
			draw.Draw(bg, bg.Rect, img, image.Point{}, draw.Over)
			f, err := os.Create(filepath.Join(dir, file))
			if err != nil {
				return err
			}
			err = png.Encode(f, bg)
			f.Close()
			if err != nil {
				return err
			}
		}
	}
	return nil
}
