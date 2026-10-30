package app

import (
	"time"

	"github.com/nooblk-98/burnbar/internal/usage"
)

func (state *fetchState) beginFetch(now time.Time, manual bool) bool {
	if !manual && now.Before(state.cooldownUntil) {
		return false
	}
	if !state.fetching.CompareAndSwap(false, true) {
		return false
	}
	state.lastFetch = now
	state.activePending = false // this fetch covers any activity already queued
	return true
}

// onActivity queues only the provider whose session logs changed.
func (a *app) onActivity(p usage.Provider) {
	if !a.enabled(p) {
		return
	}
	a.fetches[p].activePending = true
	a.refreshActivity()
}

// pendingActivity rechecks the latest fetch and back-off times when the timer
// fires. A manual or periodic fetch may have happened since it was scheduled.
func (a *app) pendingActivity(now time.Time) (ready []usage.Provider, wait time.Duration) {
	for _, p := range usage.Providers("both") {
		state := a.fetches[p]
		if !state.activePending {
			continue
		}
		if !a.enabled(p) {
			state.activePending = false
			continue
		}
		next := state.lastFetch.Add(activeMinGap)
		if state.cooldownUntil.After(next) {
			next = state.cooldownUntil
		}
		delay := next.Sub(now)
		if state.fetching.Load() {
			// Retain activity seen during a fetch, without spinning on a busy one.
			delay = max(delay, time.Second)
		}
		if delay <= 0 {
			state.activePending = false
			ready = append(ready, p)
		} else if wait == 0 || delay < wait {
			wait = delay
		}
	}
	return ready, wait
}

func (a *app) refreshActivity() {
	pKillTimer.Call(a.main, timerActive)
	ready, wait := a.pendingActivity(time.Now())
	for _, p := range ready {
		a.refreshProvider(p, false)
	}
	if wait > 0 {
		a.oneShot(timerActive, wait)
	}
}
