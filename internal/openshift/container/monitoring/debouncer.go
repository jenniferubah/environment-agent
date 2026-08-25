package monitoring

import (
	"sync"
	"time"
)

// Debouncer coalesces rapid status change events within a time window,
// publishing only the last event once the window elapses.
type Debouncer struct {
	interval  time.Duration
	publishFn func(StatusEvent)
	mu        sync.Mutex
	wg        sync.WaitGroup
	timers    map[string]*time.Timer
	stopped   bool
}

// NewDebouncer creates a Debouncer with the given interval and publish callback.
func NewDebouncer(interval time.Duration, publishFn func(StatusEvent)) *Debouncer {
	return &Debouncer{
		interval:  interval,
		publishFn: publishFn,
		timers:    make(map[string]*time.Timer),
	}
}

// Submit queues a status event for the given instance. If another event
// arrives within the debounce window, the previous event is replaced.
func (d *Debouncer) Submit(instanceID string, event StatusEvent) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.stopped {
		return
	}

	if t, ok := d.timers[instanceID]; ok {
		t.Stop()
	}

	d.timers[instanceID] = time.AfterFunc(d.interval, func() {
		d.mu.Lock()
		if d.stopped {
			d.mu.Unlock()
			return
		}
		delete(d.timers, instanceID)
		d.wg.Add(1)
		d.mu.Unlock()
		defer d.wg.Done()
		d.publishFn(event)
	})
}

// Stop halts the debouncer, preventing new event submissions, and waits
// for any in-flight publish callback to complete before returning.
func (d *Debouncer) Stop() {
	d.mu.Lock()
	d.stopped = true
	for id, t := range d.timers {
		t.Stop()
		delete(d.timers, id)
	}
	d.mu.Unlock()
	d.wg.Wait()
}
