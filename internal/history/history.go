// Package history keeps a week of usage readings for the popup's charts.
package history

import (
	"bufio"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nooblk-98/burnbar/internal/usage"
)

const (
	keep         = 8 * 24 * time.Hour
	compactEvery = time.Hour
	maxLine      = 64 * 1024
)

// Sample is one fetched reading; S and W are the session and weekly percentages (-1 if unknown).
type Sample struct {
	T int64   `json:"t"`
	S float64 `json:"s"`
	W float64 `json:"w"`
}

type History struct {
	mu          sync.Mutex
	path        string // empty in demo mode: nothing is written
	samples     []Sample
	nextCompact time.Time
	readFailed  bool // never replace a file whose contents were not fully read
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
	h := &History{path: filepath.Join(dir, name), nextCompact: time.Now().Add(compactEvery)}
	f, err := os.Open(h.path)
	if err != nil {
		h.readFailed = !os.IsNotExist(err)
		return h
	}
	readErr := h.read(f, time.Now().Add(-keep).Unix())
	closeErr := f.Close()
	h.readFailed = readErr != nil || closeErr != nil
	if !h.readFailed {
		h.rewrite()
	}
	return h
}

// read bounds memory for damaged lines, then continues with later readings.
// Actual I/O failures are returned so the caller can preserve the original file.
func (h *History) read(r io.Reader, cutoff int64) error {
	reader := bufio.NewReader(r)
	var line []byte
	discard := false
	for {
		part, err := reader.ReadSlice('\n')
		if !discard {
			if len(line)+len(part) > maxLine {
				line, discard = nil, true
			} else {
				line = append(line, part...)
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil && err != io.EOF {
			return err
		}
		var s Sample
		if !discard && json.Unmarshal(line, &s) == nil && s.T >= cutoff {
			h.samples = append(h.samples, s)
		}
		if err == io.EOF {
			return nil
		}
		line, discard = line[:0], false
	}
}

// rewrite replaces the file only after every retained sample was safely written.
// The caller holds mu, or is still loading the history before sharing it.
func (h *History) rewrite() error {
	f, err := os.CreateTemp(filepath.Dir(h.path), ".history-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	enc := json.NewEncoder(f)
	for _, s := range h.samples {
		if err := enc.Encode(s); err != nil {
			f.Close()
			return err
		}
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), h.path)
}

func (h *History) prune(cutoff int64) {
	kept := h.samples[:0]
	for _, s := range h.samples {
		if s.T >= cutoff {
			kept = append(kept, s)
		}
	}
	h.samples = kept
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
	defer h.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-keep).Unix()
	h.prune(cutoff)
	if s.T >= cutoff {
		h.samples = append(h.samples, s)
	}
	if h.path == "" {
		return
	}
	// Compact at most hourly instead of rewriting eight days of data per fetch.
	// A failed read must never lead to replacing unseen readings with our cache.
	if !h.readFailed && !now.Before(h.nextCompact) {
		h.nextCompact = now.Add(compactEvery)
		if h.rewrite() == nil {
			return // the replacement already contains this reading
		}
	}
	if s.T < cutoff {
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
