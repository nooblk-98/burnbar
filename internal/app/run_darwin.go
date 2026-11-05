//go:build darwin && cgo

package app

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework Foundation
#include <stdlib.h>
#include "macos.h"
*/
import "C"
import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/nooblk-98/burnbar/internal/alerts"
	"github.com/nooblk-98/burnbar/internal/config"
	"github.com/nooblk-98/burnbar/internal/history"
	"github.com/nooblk-98/burnbar/internal/insights"
	"github.com/nooblk-98/burnbar/internal/render"
	"github.com/nooblk-98/burnbar/internal/updates"
	"github.com/nooblk-98/burnbar/internal/usage"
	"golang.org/x/sys/unix"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type macResult struct {
	provider usage.Provider
	reading  usage.Usage
}
type macApp struct {
	cfg            config.Config
	demo           bool
	version        string
	readings       map[usage.Provider]usage.Usage
	hist           map[usage.Provider]*history.History
	pending        chan macResult
	fetching       map[usage.Provider]bool
	next           map[usage.Provider]time.Time
	backoff        map[usage.Provider]time.Duration
	notice, update string
	updateURL      string
	updateMu       sync.Mutex
	lastUpdate     time.Time
}

// AppKit must own the process main thread, before main performs any I/O.
func init() { runtime.LockOSThread() }

var mac *macApp

func Run(demo bool, mode, version string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	lock, e := os.OpenFile(filepath.Join(config.Dir(), "instance.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer lock.Close()
	if e = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		return fmt.Errorf("BurnBar is already running")
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	// Finder applications do not inherit an interactive shell's PATH.
	home, _ := os.UserHomeDir()
	os.Setenv("PATH", os.Getenv("PATH")+string(os.PathListSeparator)+strings.Join([]string{"/opt/homebrew/bin", "/usr/local/bin", filepath.Join(home, ".local", "bin"), filepath.Join(home, ".npm-global", "bin")}, string(os.PathListSeparator)))
	c := config.Load()
	if mode != "" {
		c.Providers = mode
	}
	mac = &macApp{cfg: c, demo: demo, version: version, readings: map[usage.Provider]usage.Usage{}, hist: map[usage.Provider]*history.History{}, pending: make(chan macResult, 4), fetching: map[usage.Provider]bool{}, next: map[usage.Provider]time.Time{}, backoff: map[usage.Provider]time.Duration{}}
	for _, p := range usage.Providers("both") {
		if demo {
			mac.hist[p] = history.Demo()
			mac.readings[p] = usage.DemoProvider(p)
		} else {
			mac.hist[p] = history.LoadProvider(config.Dir(), p)
			mac.readings[p] = usage.Usage{Provider: p, Err: "Loading…"}
		}
	}
	smoke := 0
	if os.Getenv("BURNBAR_MACOS_SMOKE") == "1" {
		if !demo {
			return fmt.Errorf("UI smoke mode requires -demo")
		}
		smoke = 1
	}
	if C.bb_run(C.int(smoke)) != 0 {
		return fmt.Errorf("macOS UI smoke check failed")
	}
	return nil
}
func (m *macApp) fetch(p usage.Provider) {
	if m.demo || m.fetching[p] || time.Now().Before(m.next[p]) {
		return
	}
	m.fetching[p] = true
	go func() { m.pending <- macResult{p, usage.FetchProvider(p)} }()
}

//export bb_tick
func bb_tick() {
	m := mac
	now := time.Now()
	for {
		select {
		case result := <-m.pending:
			p, u := result.provider, result.reading
			m.fetching[p] = false
			u = usage.PreserveLastGood(u, m.readings[p])
			if u.RateLimited {
				m.backoff[p] = min(max(2*m.backoff[p], 2*time.Minute), 16*time.Minute)
				m.next[p] = now.Add(m.backoff[p])
				if u.RetryAt.After(m.next[p]) {
					m.next[p] = u.RetryAt
				}
				u.RetryAt = m.next[p]
			} else {
				m.next[p] = now.Add(time.Duration(m.cfg.IntervalMinutes) * time.Minute)
				if u.Err == "" {
					m.backoff[p] = 0
				}
			}
			m.readings[p] = u
			if u.Err == "" {
				m.hist[p].Add(u)
				events, changed := m.cfg.MilestoneState.ObserveThresholds(u, m.cfg.PrimaryThresholds, m.cfg.SecondaryThresholds)
				if changed {
					m.cfg.Save()
				}
				if len(events) > 0 && m.cfg.MilestonePopups && !m.quiet() {
					var lines []string
					for _, e := range events {
						lines = append(lines, fmt.Sprintf("%s %s: %.0f%% used (%d%% crossed)", p.Name(), e.Label, e.Percent, e.Level))
					}
					m.notice = strings.Join(lines, "\n")
				}
			}
		default:
			goto drained
		}
	}
drained:
	for _, p := range usage.Providers(m.cfg.Providers) {
		m.fetch(p)
	}
	if !m.demo && m.cfg.CheckUpdates && (m.lastUpdate.IsZero() || now.Sub(m.lastUpdate) >= 24*time.Hour) {
		m.checkUpdates()
	}
}
func (m *macApp) quiet() bool {
	return alerts.Quiet(time.Now(), m.cfg.SnoozeUntil, m.cfg.QuietHours, m.cfg.QuietStart, m.cfg.QuietEnd)
}
func (m *macApp) checkUpdates() {
	m.lastUpdate = time.Now()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		r, e := updates.Check(ctx, &http.Client{Timeout: 15 * time.Second})
		m.updateMu.Lock()
		defer m.updateMu.Unlock()
		if e != nil {
			m.update = "Update check unavailable; try later"
		} else {
			m.update = "Latest stable: " + r.Version
			m.updateURL = r.URL
			if updates.Newer(m.version, r.Version) {
				m.update = "Update available: " + r.Version
			}
		}
	}()
}

//export bb_action
func bb_action(action C.int) {
	switch int(action) {
	case 1:
		for _, p := range usage.Providers(mac.cfg.Providers) {
			if !mac.readings[p].RateLimited {
				mac.next[p] = time.Time{}
			}
			mac.fetch(p)
		}
	case 2:
		mac.cfg.SnoozeUntil = time.Now().Add(time.Hour).Unix()
		mac.cfg.Save()
	case 3:
		mac.cfg.SnoozeUntil = 0
		mac.cfg.Save()
	case 5:
		p := usage.Claude
		if !mac.fetching[p] && (!mac.readings[p].RateLimited || !time.Now().Before(mac.next[p])) {
			usage.AllowClaudeKeychain()
			if !mac.readings[p].RateLimited {
				mac.next[p] = time.Time{}
			}
			mac.fetch(p)
		}
	case 4:
		if time.Since(mac.lastUpdate) >= time.Minute {
			mac.checkUpdates()
		}
	}
}

//export bb_state
func bb_state() *C.char {
	m := mac
	var lines []string
	meters := []map[string]any{}
	for _, p := range usage.Providers(m.cfg.Providers) {
		u := m.readings[p]
		lines = append(lines, u.Name()+" usage", insights.Freshness(u, time.Now()))
		if health := insights.Health(u, time.Now(), max(10*time.Minute, time.Duration(m.cfg.IntervalMinutes)*2*time.Minute)); health != "" {
			lines = append(lines, health)
		}
		for i, w := range []*usage.Window{u.FiveHour, u.SevenDay} {
			if w == nil {
				continue
			}
			meters = append(meters, map[string]any{"label": p.Name() + " " + usage.WindowShortLabel(w, "quota"), "percent": w.Percent})
			lines = append(lines, fmt.Sprintf("%s: %.0f%% used. %s", w.Label, w.Percent, render.ResetText(w.ResetsAt)))
			if m.cfg.ShowForecast {
				if f, ok := insights.Estimate(u, m.hist[p].Since(time.Now().Add(-3*time.Hour)), i == 1, time.Now()); ok {
					lines = append(lines, f.Text())
				}
			}
		}
		if u.Err != "" {
			lines = append(lines, u.Err)
		}
		lines = append(lines, "")
	}
	m.updateMu.Lock()
	update, url := m.update, m.updateURL
	m.updateMu.Unlock()
	state := map[string]any{"summary": strings.Join(lines, "\n"), "meters": meters, "notice": m.notice, "seconds": m.cfg.PopupSeconds, "config": m.cfg, "version": m.version, "update": update, "releaseURL": url}
	m.notice = ""
	b, _ := json.Marshal(state)
	return C.CString(string(b))
}

//export bb_save
func bb_save(raw *C.char) *C.char {
	c := mac.cfg
	var input struct {
		Providers, Primary, Secondary, QuietStart, QuietEnd string
		Seconds, Interval                                   int
		Milestones, QuietHours, Forecast, Updates           bool
	}
	if e := json.Unmarshal([]byte(C.GoString(raw)), &input); e != nil {
		return C.CString("Invalid settings")
	}
	if !usage.ValidMode(input.Providers) || input.Seconds < 1 || input.Seconds > 300 || input.Interval < 1 || input.Interval > 60 {
		return C.CString("Choose valid providers; close delay 1–300 seconds; refresh 1–60 minutes")
	}
	var e error
	c.PrimaryThresholds, e = alerts.ParseThresholds(input.Primary)
	if e != nil {
		return C.CString(e.Error())
	}
	c.SecondaryThresholds, e = alerts.ParseThresholds(input.Secondary)
	if e != nil {
		return C.CString(e.Error())
	}
	c.QuietStart, e = alerts.ParseClock(input.QuietStart)
	if e != nil {
		return C.CString(e.Error())
	}
	c.QuietEnd, e = alerts.ParseClock(input.QuietEnd)
	if e != nil {
		return C.CString(e.Error())
	}
	c.Providers = input.Providers
	c.PopupSeconds = input.Seconds
	c.IntervalMinutes = input.Interval
	c.MilestonePopups = input.Milestones
	c.QuietHours = input.QuietHours
	c.ShowForecast = input.Forecast
	c.CheckUpdates = input.Updates
	// Rebaseline only on explicit save, never replay thresholds during editing.
	c.MilestoneState = alerts.Tracker{}
	if !mac.demo {
		for _, p := range usage.Providers(c.Providers) {
			c.MilestoneState.ObserveThresholds(mac.readings[p], c.PrimaryThresholds, c.SecondaryThresholds)
		}
	}
	if e = c.SaveChecked(); e != nil {
		return C.CString("Could not save settings")
	}
	mac.cfg = c
	for _, p := range usage.Providers(c.Providers) {
		if !mac.readings[p].RateLimited {
			next := time.Now().Add(time.Duration(c.IntervalMinutes) * time.Minute)
			if mac.next[p].After(next) {
				mac.next[p] = next
			}
		}
	}
	return nil
}
