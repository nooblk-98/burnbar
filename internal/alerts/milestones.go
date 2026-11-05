// Package alerts tracks quota milestones independently of the Windows UI.
package alerts

import (
	"github.com/nooblk-98/burnbar/internal/usage"
	"time"
)

type MilestoneState struct {
	Reset     int64    `json:"reset"`
	Length    int64    `json:"length"`
	Level     int      `json:"level"`
	HighWater *float64 `json:"highWater,omitempty"`
}

type Milestone struct {
	Provider usage.Provider
	Label    string
	Level    int
	Window   int
	Percent  float64
	ResetsAt time.Time
}

type Tracker map[string]MilestoneState

// Observe uses the first valid reading of each quota window as a baseline, not
// a backlog of alerts. Only a new high-water decade in the same window alerts.
// Persisting this state prevents polling, corrections and restarts replaying it.
func (t Tracker) Observe(u usage.Usage) ([]Milestone, bool) {
	return t.ObserveThresholds(u, DefaultThresholds(), DefaultThresholds())
}

func (t Tracker) ObserveThresholds(u usage.Usage, primary, secondary []int) (events []Milestone, changed bool) {
	if u.Err != "" || u.RateLimited {
		return nil, false
	}
	for i, w := range []*usage.Window{u.FiveHour, u.SevenDay} {
		if w == nil || !(w.Percent >= 0 && w.Percent <= 100) {
			continue
		}
		key := w.Key
		if key == "" {
			key = []string{"primary", "secondary"}[i]
		}
		key = string(u.Provider) + "/" + key
		reset := int64(0)
		if !w.ResetsAt.IsZero() {
			reset = w.ResetsAt.Truncate(time.Minute).Unix()
		}
		prev, seen := t[key]
		high := float64(prev.Level)
		if prev.HighWater != nil {
			high = *prev.HighWater
		}
		same := seen && prev.Reset == reset && prev.Length == int64(w.Length)
		if same && reset == 0 && w.Percent < 10 && high >= 10 {
			same = false
		}
		nextHigh := w.Percent
		if same {
			thresholds := primary
			if i == 1 {
				thresholds = secondary
			}
			level := 0
			for _, value := range thresholds {
				if float64(value) > high && float64(value) <= w.Percent {
					level = max(level, value)
				}
			}
			if level > 0 {
				events = append(events, Milestone{Provider: u.Provider, Label: usage.WindowShortLabel(w, []string{"primary", "secondary"}[i]), Level: level, Window: i, Percent: w.Percent, ResetsAt: w.ResetsAt})
			}
			nextHigh = max(high, w.Percent)
		}
		cur := MilestoneState{Reset: reset, Length: int64(w.Length), Level: int(nextHigh/10) * 10, HighWater: &nextHigh}
		if !seen || prev.Reset != cur.Reset || prev.Length != cur.Length || high != nextHigh || prev.HighWater == nil {
			t[key] = cur
			changed = true
		}
	}
	return
}
