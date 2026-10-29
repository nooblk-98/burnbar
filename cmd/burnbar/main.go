// Command burnbar shows Claude and Codex plan usage on the Windows taskbar.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nooblk-98/burnbar/internal/app"
	"github.com/nooblk-98/burnbar/internal/config"
	"github.com/nooblk-98/burnbar/internal/history"
	"github.com/nooblk-98/burnbar/internal/render"
	"github.com/nooblk-98/burnbar/internal/usage"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	demo := flag.Bool("demo", false, "show sample numbers instead of calling the usage API")
	once := flag.Bool("once", false, "print current usage to stdout and exit")
	snapshot := flag.String("snapshot", "", "write widget and popup PNGs to this folder and exit")
	providers := flag.String("providers", "", "usage providers: claude, codex or both (default: saved setting)")
	taskbarLayout := flag.Bool("taskbar-layout", false, "print taskbar occupied spans and compact meter placement, then exit")
	flag.Parse()
	usage.UserAgent = "burnbar/" + version
	if *providers != "" && !usage.ValidMode(*providers) {
		fmt.Fprintln(os.Stderr, "-providers must be claude, codex or both")
		os.Exit(1)
	}
	mode := *providers
	if mode == "" {
		mode = config.Load().Providers
	}

	var err error
	switch {
	case *taskbarLayout:
		err = app.WriteTaskbarLayout(os.Stdout)
	case *once:
		err = printOnce(mode, *demo)
	case *snapshot != "":
		err = writeSnapshots(*snapshot, *demo, mode)
	default:
		err = app.Run(*demo, *providers)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func printOnce(mode string, demo bool) error {
	var failures []string
	for _, p := range usage.Providers(mode) {
		u := usage.DemoProvider(p)
		if !demo {
			u = usage.FetchProvider(p)
		}
		fmt.Println(render.Tooltip(u))
		for _, w := range u.Extra {
			fmt.Printf("%s: %.0f%%\n", w.Label, w.Percent)
		}
		fmt.Println()
		if u.Err != "" {
			failures = append(failures, p.Name()+": "+u.Err)
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

// writeSnapshots renders the widget and popup in both themes, for checking the design.
func writeSnapshots(dir string, demo bool, mode string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var readings []usage.Usage
	var panels []render.Panel
	for _, p := range usage.Providers(mode) {
		u, hist := usage.DemoProvider(p), history.Demo()
		if !demo {
			u, hist = usage.FetchProvider(p), history.LoadProvider(config.Dir(), p)
		}
		readings = append(readings, u)
		panels = append(panels, render.Panel{Usage: u, History: hist.Since(time.Now().Add(-7 * 24 * time.Hour))})
	}
	for _, light := range []bool{false, true} {
		name := map[bool]string{false: "dark", true: "light"}[light]
		bgColor := map[bool]color.RGBA{false: {0x20, 0x20, 0x20, 0xFF}, true: {0xEE, 0xF0, 0xF6, 0xFF}}[light]
		th := render.NewTheme(light)
		imgs := map[string]*image.RGBA{
			"widget-" + name + ".png":       render.GroupWidget(1.25, readings, th, false),
			"widget-hover-" + name + ".png": render.GroupWidget(1.25, readings, th, true),
			"popup-" + name + ".png":        render.GroupPopup(1.25, panels, th, 1400, 1000),
		}
		if len(readings) > 0 {
			imgs["widget-narrow-"+name+".png"] = render.ProviderWidgetSized(1.25, readings[0], th, false, render.MinimumWidgetWidth(1.25))
			imgs["widget-floating-"+name+".png"] = render.FloatingWidget(render.ProviderWidget(1.25, readings[0], th, false), th, 1.25)
		}
		if len(readings) > 1 {
			from := render.ProviderWidget(1.25, readings[0], th, false)
			to := render.ProviderWidget(1.25, readings[1], th, false)
			imgs["widget-next-"+name+".png"] = to
			for _, percent := range []int{25, 50, 75} {
				imgs[fmt.Sprintf("flip-%d-%s.png", percent, name)] = render.FlipDown(from, to, float64(percent)/100)
			}
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
		if len(readings) > 1 {
			if err := writeRotationGIF(filepath.Join(dir, "meter-rotation-"+name+".gif"), readings, th, bgColor); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeRotationGIF records the same renderer and flip used by the taskbar, with
// demo readings when -demo is selected. It never captures the user's desktop.
func writeRotationGIF(path string, readings []usage.Usage, th render.Theme, bgColor color.RGBA) error {
	const scale = 2.5
	from := render.ProviderWidget(scale, readings[0], th, false)
	to := render.ProviderWidget(scale, readings[1], th, false)
	colors := color.Palette{bgColor, color.RGBA{0xE8, 0x74, 0x3B, 255}, color.RGBA{0x3F, 0xB9, 0x7A, 255}, color.RGBA{0xE5, 0x48, 0x4D, 255}}
	colors = append(colors, palette.Plan9[:252]...)
	animation := &gif.GIF{LoopCount: 0}
	appendFrame := func(img *image.RGBA, delay int) {
		bg := image.NewRGBA(img.Rect)
		draw.Draw(bg, bg.Rect, image.NewUniform(bgColor), image.Point{}, draw.Src)
		draw.Draw(bg, bg.Rect, img, image.Point{}, draw.Over)
		frame := image.NewPaletted(img.Rect, colors)
		draw.FloydSteinberg.Draw(frame, frame.Rect, bg, image.Point{})
		animation.Image = append(animation.Image, frame)
		animation.Delay = append(animation.Delay, delay)
		animation.Disposal = append(animation.Disposal, gif.DisposalNone)
	}
	// Rotation starts every eight seconds; 42 centiseconds belong to the flip.
	for _, pair := range [][2]*image.RGBA{{from, to}, {to, from}} {
		appendFrame(pair[0], 758)
		for i := 1; i <= 14; i++ {
			appendFrame(render.FlipDown(pair[0], pair[1], float64(i)/14), 3)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	err = gif.EncodeAll(f, animation)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
