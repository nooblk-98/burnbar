// Package insights derives conservative, local-only status and pace information.
package insights

import (
	"fmt"
	"github.com/nooblk-98/burnbar/internal/history"
	"github.com/nooblk-98/burnbar/internal/usage"
	"math"
	"strings"
	"time"
)

func Age(at, now time.Time) string {
	if at.IsZero() {
		return "Never updated"
	}
	d := max(time.Duration(0), now.Sub(at))
	if d < time.Minute {
		return "Updated just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("Updated %d min ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("Updated %dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("Updated %dd ago", int(d.Hours()/24))
}

func Health(u usage.Usage, now time.Time, staleAfter time.Duration) string {
	if u.Err != "" {
		e := strings.ToLower(u.Err)
		switch {
		case strings.Contains(e, "loading"):
			return "Loading"
		case u.RateLimited:
			return "Rate limited — retrying"
		case strings.Contains(e, "login"), strings.Contains(e, "sign in"), strings.Contains(e, "token"), strings.Contains(e, "credentials"):
			return "Sign-in required"
		case strings.Contains(e, "network"), strings.Contains(e, "timeout"), strings.Contains(e, "connection"), strings.Contains(e, "dial "):
			return "Offline / connection error"
		default:
			return "Usage unavailable"
		}
	}
	if len(u.All()) == 0 || u.UpdatedAt.IsZero() {
		return "Waiting for usage"
	}
	if now.Sub(u.UpdatedAt) > staleAfter {
		return "Stale reading"
	}
	return ""
}

type Forecast struct {
	Remaining   time.Duration
	BeforeReset bool
}

// Estimate requires at least three contiguous observations covering 20 minutes,
// growth >=1 percentage point, and a known current quota cycle. Reset/correction
// drops and long gaps cut off history; stale/error readings never get forecasts.
func Estimate(u usage.Usage, samples []history.Sample, secondary bool, now time.Time) (Forecast, bool) {
	w := u.FiveHour
	if secondary {
		w = u.SevenDay
	}
	if u.Err != "" || u.RateLimited || w == nil || w.Length <= 0 || w.ResetsAt.IsZero() || !w.ResetsAt.After(now) || now.Sub(u.UpdatedAt) > 10*time.Minute || u.UpdatedAt.After(now.Add(time.Minute)) || !(w.Percent >= 0 && w.Percent < 100) {
		return Forecast{}, false
	}
	start := w.ResetsAt.Add(-w.Length)
	cutoff := maxTime(start, now.Add(-3*time.Hour))
	anchor := u.UpdatedAt.Truncate(time.Second)
	lastT, lastP := anchor, w.Percent
	firstT, firstP := lastT, lastP
	count := 1
	// History is chronological; traverse backwards without bridging a reset or
	// correction. Equal timestamps (the current reading) are not extra samples.
	for i := len(samples) - 1; i >= 0; i-- {
		s := samples[i]
		t := time.Unix(s.T, 0)
		p := s.S
		if secondary {
			p = s.W
		}
		if !t.Before(lastT) {
			continue
		}
		if t.Before(cutoff) {
			break
		}
		if p < 0 || p > 100 || math.IsNaN(p) || lastP < p || lastT.Sub(t) > 20*time.Minute {
			break
		}
		firstT, firstP = t, p
		lastT, lastP = t, p
		count++
	}
	span := anchor.Sub(firstT)
	growth := w.Percent - firstP
	if count < 3 || span < 20*time.Minute || growth < 1 {
		return Forecast{}, false
	}
	duration := time.Duration((100 - w.Percent) / growth * float64(span))
	if duration <= 0 {
		return Forecast{}, false
	}
	return Forecast{Remaining: duration, BeforeReset: now.Add(duration).Before(w.ResetsAt)}, true
}
func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
func (f Forecast) Text() string {
	if !f.BeforeReset {
		return "Recent pace: reset expected before limit"
	}
	minutes := max(1, int(math.Round(f.Remaining.Minutes()/5)*5))
	duration := fmt.Sprintf("%d min", minutes)
	if minutes >= 60 {
		duration = fmt.Sprintf("%dh %02dm", minutes/60, minutes%60)
	}
	return "At recent pace: ~" + duration + " until limit"
}
