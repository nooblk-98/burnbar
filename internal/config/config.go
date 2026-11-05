// Package config stores the meter's settings and alert state in %APPDATA%\BurnBar.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/nooblk-98/burnbar/internal/alerts"
	"github.com/nooblk-98/burnbar/internal/usage"
)

type Config struct {
	Providers            string         `json:"providers"` // "claude", "codex" or "both"
	PopupSeconds         int            `json:"popupSeconds"`
	PopupBehaviorVersion int            `json:"popupBehaviorVersion"`
	MilestonePopups      bool           `json:"milestonePopups"`
	MilestoneState       alerts.Tracker `json:"milestoneState,omitempty"`
	AlwaysShowPopup      bool           `json:"alwaysShowPopup"`
	IntervalMinutes      int            `json:"intervalMinutes"`
	Alerts               bool           `json:"alerts"`
	// Nil identifies a legacy profile whose preference still lives in the registry.
	StartWithWindows *bool `json:"startWithWindows,omitempty"`

	PrimaryThresholds   []int  `json:"primaryThresholds"`
	SecondaryThresholds []int  `json:"secondaryThresholds"`
	SnoozeUntil         int64  `json:"snoozeUntil,omitempty"`
	QuietHours          bool   `json:"quietHours"`
	QuietStart          int    `json:"quietStart"`
	QuietEnd            int    `json:"quietEnd"`
	RespectWindowsQuiet bool   `json:"respectWindowsQuiet"`
	CompactNotices      bool   `json:"compactNotices"`
	NativeNotices       bool   `json:"nativeNotices"`
	ShowForecast        bool   `json:"showForecast"`
	Theme               string `json:"theme"`
	TextScale           int    `json:"textScale"`
	Hotkey              bool   `json:"hotkey"`
	CheckUpdates        bool   `json:"checkUpdates"`
	// AlertState remembers, per limit, which thresholds were already announced in the current window.
	AlertState map[string]AlertState `json:"alertState,omitempty"`
}

type AlertState struct {
	ResetsAt int64   `json:"resetsAt"`
	Level    int     `json:"level"`
	Percent  float64 `json:"percent"`
}

func Default() Config {
	startup := true
	return Config{Providers: "both", PopupSeconds: alerts.DefaultPopupSeconds, PopupBehaviorVersion: 1, MilestoneState: alerts.Tracker{}, MilestonePopups: true, AlwaysShowPopup: false, IntervalMinutes: 3, Alerts: true, PrimaryThresholds: alerts.DefaultThresholds(), SecondaryThresholds: alerts.DefaultThresholds(), QuietStart: 22 * 60, QuietEnd: 8 * 60, RespectWindowsQuiet: true, CompactNotices: true, ShowForecast: true, Theme: "system", TextScale: 100, CheckUpdates: true, StartWithWindows: &startup}
}

// Dir is the folder holding settings and history; it is created if missing.
func Dir() string {
	base := os.Getenv("APPDATA")
	if base == "" {
		base, _ = os.UserConfigDir()
	}
	dir := filepath.Join(base, "BurnBar")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		// Pre-rename builds used "ClaudeUsageMeter"; carry their settings and history over.
		os.Rename(filepath.Join(base, "ClaudeUsageMeter"), dir)
	}
	os.MkdirAll(dir, 0o755)
	return dir
}

func path() string { return filepath.Join(Dir(), "config.json") }

func Load() Config {
	c := Default()
	if b, err := os.ReadFile(path()); err == nil {
		// Existing profiles without this field must retain their registry choice.
		c.StartWithWindows = nil
		c.PopupBehaviorVersion = 0
		json.Unmarshal(b, &c)
	}
	// One-time migration away from the previous always-visible default. A later
	// explicit Always show selection is retained by this behavior version.
	if c.PopupBehaviorVersion < 1 {
		c.AlwaysShowPopup = false
		c.PopupBehaviorVersion = 1
	}
	if c.PopupSeconds < 1 || c.PopupSeconds > 300 {
		c.PopupSeconds = alerts.DefaultPopupSeconds
	}
	if c.PrimaryThresholds == nil {
		c.PrimaryThresholds = alerts.DefaultThresholds()
	}
	c.PrimaryThresholds = alerts.NormalizeThresholds(c.PrimaryThresholds)
	if c.SecondaryThresholds == nil {
		c.SecondaryThresholds = alerts.DefaultThresholds()
	}
	c.SecondaryThresholds = alerts.NormalizeThresholds(c.SecondaryThresholds)
	if c.QuietStart < 0 || c.QuietStart >= 1440 {
		c.QuietStart = 22 * 60
	}
	if c.QuietEnd < 0 || c.QuietEnd >= 1440 {
		c.QuietEnd = 8 * 60
	}
	if c.TextScale != 100 && c.TextScale != 125 && c.TextScale != 150 {
		c.TextScale = 100
	}
	if c.Theme != "system" && c.Theme != "light" && c.Theme != "dark" {
		c.Theme = "system"
	}
	if c.MilestoneState == nil {
		c.MilestoneState = alerts.Tracker{}
	}
	if c.IntervalMinutes < 1 {
		c.IntervalMinutes = 3
	}
	if !usage.ValidMode(c.Providers) {
		c.Providers = "both"
	}
	if c.AlertState == nil {
		c.AlertState = map[string]AlertState{}
	}
	return c
}

func (c Config) Save() { _ = c.SaveChecked() }

func (c Config) SaveChecked() error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path())
}
