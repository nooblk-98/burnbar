// Command burnbar shows Claude and Codex plan usage in the Windows notification area.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nooblk-98/burnbar/internal/alerts"
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
	snapshot := flag.String("snapshot", "", "write popup PNGs to this folder and exit")
	providers := flag.String("providers", "", "usage providers: claude, codex or both (default: saved setting)")
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
	case *once:
		err = printOnce(mode, *demo)
	case *snapshot != "":
		err = writeSnapshots(*snapshot, *demo, mode)
	default:
		err = app.Run(*demo, *providers, version)
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

// writeSnapshots renders the popups in both themes, for checking the design.
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
		panels = append(panels, render.Panel{Usage: u, ShowForecast: true, History: hist.Since(time.Now().Add(-7 * 24 * time.Hour))})
	}
	for _, theme := range []struct {
		name  string
		theme render.Theme
	}{{"dark", render.NewTheme(false)}, {"light", render.NewTheme(true)}, {"high-contrast", render.HighContrastTheme(color.NRGBA{A: 255}, color.NRGBA{R: 255, G: 255, B: 255, A: 255}, color.NRGBA{R: 255, G: 255, A: 255})}} {
		name, th := theme.name, theme.theme
		imgs := map[string]*image.RGBA{"popup-" + name + ".png": render.GroupPopup(1.25, panels, th, 1400, 1000)}
		card := render.TrayCard(1.25, readings, th)
		imgs["tray-popup-"+name+".png"] = render.TrayBubble(card, 1.25, th, card.Rect.Dx()-35, false)
		if len(readings) > 0 {
			notice := render.CompactNotice(1.25, []alerts.Milestone{{Provider: readings[0].Provider, Label: "5h", Level: 60, Percent: 62, ResetsAt: readings[0].FiveHour.ResetsAt}}, th)
			imgs["milestone-popup-"+name+".png"] = render.TrayBubble(notice, 1.25, th, notice.Rect.Dx()-35, false)
		}
		for file, img := range imgs {
			f, err := os.Create(filepath.Join(dir, file))
			if err != nil {
				return err
			}
			err = png.Encode(f, img)
			f.Close()
			if err != nil {
				return err
			}
		}
	}
	return nil
}
