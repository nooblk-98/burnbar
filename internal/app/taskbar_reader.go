package app

import (
	"runtime"
	"sync"
	"time"

	"github.com/nooblk-98/burnbar/internal/layout"
)

type taskbarSource interface {
	items(uintptr) ([]layout.Item, bool)
	close()
}

type taskbarReading struct {
	items []layout.Item // immutable; no COM pointers leave the reader thread
	ok    bool
	at    time.Time
}

// taskbarReader isolates synchronous Explorer RPCs from the window thread.
// One bounded worker handles all monitors, even if an Explorer request stalls.
type taskbarReader struct {
	mu       sync.Mutex
	readings map[uintptr]taskbarReading
	pending  map[uintptr]bool
	requests chan uintptr
	stop     chan struct{}
	closed   bool
}

func newTaskbarReader(notify func(), open func() taskbarSource) *taskbarReader {
	r := &taskbarReader{
		readings: make(map[uintptr]taskbarReading), pending: make(map[uintptr]bool),
		requests: make(chan uintptr, 32), stop: make(chan struct{}),
	}
	go r.run(notify, open)
	return r
}

func (r *taskbarReader) run(notify func(), open func() taskbarSource) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	source := open()
	defer source.close()
	for {
		select {
		case <-r.stop:
			return
		case hwnd := <-r.requests:
			select {
			case <-r.stop:
				return
			default:
			}
			items, ok := source.items(hwnd) // never hold the cache lock during RPC
			r.mu.Lock()
			delete(r.pending, hwnd)
			if r.closed {
				r.mu.Unlock()
				return
			}
			r.readings[hwnd] = taskbarReading{items: items, ok: ok, at: time.Now()}
			r.mu.Unlock()
			notify()
		}
	}
}

func (r *taskbarReader) request(hwnd uintptr, maxAge time.Duration) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.pending[hwnd] {
		return false
	}
	if cached, ok := r.readings[hwnd]; ok && time.Since(cached.at) < maxAge {
		return false
	}
	select {
	case r.requests <- hwnd:
		r.pending[hwnd] = true
		return true
	default:
		// A later layout tick retries; never wait for a busy Explorer worker.
		return false
	}
}

func (r *taskbarReader) snapshot(hwnd uintptr) (taskbarReading, bool) {
	if r == nil {
		return taskbarReading{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	reading, ok := r.readings[hwnd]
	return reading, ok
}

func (r *taskbarReader) forgetMissing(active map[uintptr]bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for hwnd := range r.readings {
		if !active[hwnd] {
			delete(r.readings, hwnd)
		}
	}
}

func (r *taskbarReader) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.closed {
		r.closed = true
		close(r.stop)
	}
	// Do not join the worker: an unresponsive Explorer must not block exit.
}
