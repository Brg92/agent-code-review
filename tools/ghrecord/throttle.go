package ghrecord

import "time"

// Throttle keeps requests at least Interval apart. It is not safe for
// concurrent use: the recorder never sends two searches at once.
type Throttle struct {
	Interval time.Duration
	// Now and Sleep default to time.Now and time.Sleep.
	Now   func() time.Time
	Sleep func(time.Duration)

	last time.Time
}

// Wait returns when a request may be sent. A nil Throttle never waits.
func (t *Throttle) Wait() {
	if t == nil {
		return
	}
	now, sleep := time.Now, time.Sleep
	if t.Now != nil {
		now = t.Now
	}
	if t.Sleep != nil {
		sleep = t.Sleep
	}
	if !t.last.IsZero() {
		if wait := t.Interval - now().Sub(t.last); wait > 0 {
			sleep(wait)
		}
	}
	t.last = now()
}
