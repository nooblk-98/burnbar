// Package history keeps a week of usage readings for the popup's charts.
package history

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nooblk-98/burnbar/internal/usage"
)

const keep = 8 * 24 * time.Hour

// Sample is one fetched reading; S and W are the session and weekly percentages (-1 if unknown).
type Sample struct {
	T int64   `json:"t"`
	S float64 `json:"s"`
	W float64 `json:"w"`
}

type History struct {
	mu      sync.Mutex
	path    string // empty in demo mode: nothing is written
	samples []Sample
}

// Load reads history.jsonl from dir, dropping readings older than eight days.
func Load(dir string) *History {
	return LoadProvider(dir, usage.Claude)
}

// LoadProvider keeps Claude's existing history file and isolates Codex readings.
func LoadProvider(dir string, provider usage.Provider) *History {
	name := "history.jsonl"
	if provider == usage.Codex {
		name = "history-codex.jsonl"
	}
	h := &History{path: filepath.Join(dir, name)}
	f, err := os.Open(h.path)
	if err != nil {
		return h
	}
	cutoff := time.Now().Add(-keep).Unix()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var s Sample
		if json.Unmarshal(sc.Bytes(), &s) == nil && s.T >= cutoff {
			h.samples = append(h.samples, s)
		}
	}
	f.Close()
	h.rewrite()
	return h
}

// rewrite compacts the file down to the samples still kept in memory.
func (h *History) rewrite() {
	tmp := h.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return
	}
	enc := json.NewEncoder(f)
	for _, s := range h.samples {
		enc.Encode(s)
	}
	f.Close()
	os.Rename(tmp, h.path)
}

func pct(w *usage.Window) float64 {
	if w == nil {
		return -1
	}
	return w.Percent
}

func (h *History) Add(u usage.Usage) {
	s := Sample{T: u.UpdatedAt.Unix(), S: pct(u.FiveHour), W: pct(u.SevenDay)}
	h.mu.Lock()
	h.samples = append(h.samples, s)
	h.mu.Unlock()
	if h.path == "" {
		return
	}
	if f, err := os.OpenFile(h.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		json.NewEncoder(f).Encode(s)
		f.Close()
	}
}

func (h *History) Since(t time.Time) []Sample {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []Sample
	for _, s := range h.samples {
		if s.T >= t.Unix() {
			out = append(out, s)
		}
	}
	return out
}

// Demo fabricates a plausible week of readings for -demo mode.
func Demo() *History {
	h := &History{}
	now := time.Now()
	for t := now.Add(-7 * 24 * time.Hour); t.Before(now); t = t.Add(10 * time.Minute) {
		hours := now.Sub(t).Hours()
		session := math.Max(0, 60*math.Sin(hours/5*math.Pi)) // rises and resets every ~5h
		weekly := 17 * (1 - hours/(7*24))
		h.samples = append(h.samples, Sample{T: t.Unix(), S: session, W: weekly})
	}
	return h
}
