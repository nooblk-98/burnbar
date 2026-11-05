// Package alerts tracks quota milestones independently of the Windows UI.
package alerts

import (
	"github.com/nooblk-98/burnbar/internal/usage"
	"time"
)

type MilestoneState struct {
	Reset  int64 `json:"reset"`
	Length int64 `json:"length"`
	Level  int   `json:"level"`
}

type Milestone struct {
	Provider usage.Provider
	Label    string
	Level    int
}

type Tracker map[string]MilestoneState

// Observe uses the first valid reading of each quota window as a baseline, not
// a backlog of alerts. Only a new high-water decade in the same window alerts.
// Persisting this state prevents polling, corrections and restarts replaying it.
func (t Tracker) Observe(u usage.Usage) (events []Milestone, changed bool) {
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
		cur := MilestoneState{Reset: reset, Length: int64(w.Length), Level: int(w.Percent/10) * 10}
		prev, seen := t[key]
		same := seen && prev.Reset == cur.Reset && prev.Length == cur.Length
		// If no reset is reported, seeing usage fall back below 10% rearms the quota.
		if same && reset == 0 && cur.Level == 0 && prev.Level > 0 {
			same = false
		}
		if same {
			if cur.Level > prev.Level {
				label := usage.WindowShortLabel(w, []string{"primary", "secondary"}[i])
				events = append(events, Milestone{Provider: u.Provider, Label: label, Level: cur.Level})
			}
			cur.Level = max(cur.Level, prev.Level)
		}
		if !seen || cur != prev {
			t[key] = cur
			changed = true
		}
	}
	return
}
