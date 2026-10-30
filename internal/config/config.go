// Package config stores the meter's settings and alert state in %APPDATA%\BurnBar.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/nooblk-98/burnbar/internal/usage"
)

type Config struct {
	Providers       string `json:"providers"` // "claude", "codex" or "both"
	ShowOverlay     bool   `json:"showOverlay"`
	AllMonitors     bool   `json:"allMonitors"`
	Position        string `json:"position"` // "auto", "left" or "right" (next to the tray)
	IntervalMinutes int    `json:"intervalMinutes"`
	Alerts          bool   `json:"alerts"`
	// Nil identifies a legacy profile whose preference still lives in the registry.
	StartWithWindows *bool `json:"startWithWindows,omitempty"`

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
	return Config{Providers: "both", ShowOverlay: true, AllMonitors: true, Position: "auto", IntervalMinutes: 3, Alerts: true, StartWithWindows: &startup}
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
		json.Unmarshal(b, &c)
	}
	switch c.Position {
	case "auto", "left", "right":
	default:
		c.Position = "auto"
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

func (c Config) Save() {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	tmp := path() + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, path())
	}
}
