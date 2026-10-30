package app

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/nooblk-98/burnbar/internal/config"
	"github.com/nooblk-98/burnbar/internal/layout"
)

var testGetThreadID = kernel32.NewProc("GetCurrentThreadId")

type stalledTaskbarSource struct {
	entered     chan uintptr
	release     chan struct{}
	closed      chan struct{}
	thread      uintptr
	wrongThread atomic.Bool
}

func (s *stalledTaskbarSource) checkThread() {
	id, _, _ := testGetThreadID.Call()
	if id != s.thread {
		s.wrongThread.Store(true)
	}
}

func (s *stalledTaskbarSource) items(hwnd uintptr) ([]layout.Item, bool) {
	s.checkThread()
	s.entered <- hwnd
	<-s.release
	return []layout.Item{{Class: "Button", Rect: RECT{Left: 100, Right: 150}}}, true
}

func (s *stalledTaskbarSource) close() {
	s.checkThread()
	close(s.closed)
}

func startTestReader(t *testing.T) (*taskbarReader, *stalledTaskbarSource, chan struct{}) {
	t.Helper()
	source := &stalledTaskbarSource{
		entered: make(chan uintptr, 32), release: make(chan struct{}), closed: make(chan struct{}),
	}
	notified := make(chan struct{}, 32)
	r := newTaskbarReader(func() { notified <- struct{}{} }, func() taskbarSource {
		source.thread, _, _ = testGetThreadID.Call()
		return source
	})
	t.Cleanup(func() {
		close(source.release)
		r.close()
		select {
		case <-source.closed:
		case <-time.After(time.Second):
			t.Error("reader did not release its source")
		}
		if source.wrongThread.Load() {
			t.Error("the source was used or released on another OS thread")
		}
	})
	return r, source, notified
}

func TestTaskbarReaderDoesNotBlockOnStalledExplorer(t *testing.T) {
	r, source, _ := startTestReader(t)
	r.request(11, 0)
	select {
	case <-source.entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	responsive := make(chan struct{})
	go func() {
		defer close(responsive)
		for i := 0; i < 1000; i++ {
			if r.request(11, 0) {
				t.Error("duplicate query was queued while Explorer was blocked")
			}
			r.snapshot(11)
		}
		r.close() // Exit must also remain responsive while RPC is blocked.
	}()
	select {
	case <-responsive:
	case <-time.After(time.Second):
		t.Fatal("GUI-side cache operations waited for Explorer")
	}
	if r.request(12, 0) {
		t.Fatal("a closed reader accepted a request")
	}
}

func TestTaskbarReaderCachesAndThrottlesMeasurements(t *testing.T) {
	r, source, notified := startTestReader(t)
	// Permit one request to complete without leaving the source unblocked forever.
	go func() { source.release <- struct{}{} }()
	if !r.request(11, time.Hour) {
		t.Fatal("initial request was not queued")
	}
	select {
	case <-notified:
	case <-time.After(time.Second):
		t.Fatal("completed measurement was not reported")
	}
	reading, ok := r.snapshot(11)
	if !ok || !reading.ok || len(reading.items) != 1 || reading.at.IsZero() {
		t.Fatal("completed taskbar measurement was not cached")
	}
	if r.request(11, time.Hour) {
		t.Fatal("a fresh measurement was queried again")
	}
	r.forgetMissing(map[uintptr]bool{})
	if _, ok := r.snapshot(11); ok {
		t.Fatal("removed taskbar was retained in the cache")
	}
}

func TestSlotOnlyFloatsForMeasuredCrowding(t *testing.T) {
	tb := taskbar{hwnd: 1, rect: RECT{Right: 1920, Top: 1020, Bottom: 1080}, primary: true}
	for _, tc := range []struct {
		name    string
		reading taskbarReading
		ready   bool
	}{
		{name: "unreadable taskbar"},
		{name: "stale taskbar", reading: taskbarReading{ok: true, at: time.Now().Add(-time.Minute)}},
		{name: "measured full taskbar", reading: taskbarReading{ok: true, at: time.Now(), items: []layout.Item{
			{Class: "Button", Rect: RECT{Left: 0, Right: 1400, Top: 1020, Bottom: 1080}},
			{Class: "SystemTray.NormalButton", Rect: RECT{Left: 1400, Right: 1920, Top: 1020, Bottom: 1080}},
		}}, ready: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &taskbarReader{readings: map[uintptr]taskbarReading{1: tc.reading}, pending: map[uintptr]bool{}, requests: make(chan uintptr, 1)}
			a := &app{cfg: config.Default(), uia: r}
			_, _, fits, ready := a.slot(&overlay{}, tb, 163, 1.25)
			if fits || ready != tc.ready {
				t.Fatalf("fits=%v ready=%v, want false/%v", fits, ready, tc.ready)
			}
		})
	}
}
