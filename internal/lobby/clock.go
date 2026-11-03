package lobby

import "time"

// Clock is the time source for the rate limit and the disconnect grace.
// Tests advance a fake clock instead of waiting 30 seconds.
type Clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, fn func()) Stopper
}

// Stopper cancels a delayed call.
type Stopper interface {
	Stop() bool
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type realStopper struct{ t *time.Timer }

func (realClock) AfterFunc(d time.Duration, fn func()) Stopper {
	return realStopper{t: time.AfterFunc(d, fn)}
}

func (s realStopper) Stop() bool {
	if s.t == nil {
		return false
	}
	return s.t.Stop()
}

type fakeClock struct {
	mu     chan struct{}
	now    time.Time
	next   int
	timers []*fakeTimer
}

type fakeTimer struct {
	id      int
	when    time.Time
	fn      func()
	stopped bool
	clock   *fakeClock
}

func newFakeClock(now time.Time) *fakeClock {
	return &fakeClock{mu: make(chan struct{}, 1), now: now}
}

func (f *fakeClock) lock()   { f.mu <- struct{}{} }
func (f *fakeClock) unlock() { <-f.mu }

func (f *fakeClock) Now() time.Time {
	f.lock()
	defer f.unlock()
	return f.now
}

func (f *fakeClock) AfterFunc(d time.Duration, fn func()) Stopper {
	f.lock()
	defer f.unlock()
	f.next++
	t := &fakeTimer{id: f.next, when: f.now.Add(d), fn: fn, clock: f}
	f.timers = append(f.timers, t)
	return t
}

func (t *fakeTimer) Stop() bool {
	t.clock.lock()
	defer t.clock.unlock()
	if t.stopped {
		return false
	}
	t.stopped = true
	return true
}

// Advance moves the clock forward and runs every timer that is due.
func (f *fakeClock) Advance(d time.Duration) {
	f.lock()
	f.now = f.now.Add(d)
	due := make([]func(), 0)
	keep := f.timers[:0]
	for _, t := range f.timers {
		if t.stopped {
			continue
		}
		if !t.when.After(f.now) {
			t.stopped = true
			due = append(due, t.fn)
			continue
		}
		keep = append(keep, t)
	}
	f.timers = keep
	f.unlock()
	for _, fn := range due {
		fn()
	}
}
