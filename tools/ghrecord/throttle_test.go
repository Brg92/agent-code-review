package ghrecord

import (
	"reflect"
	"testing"
	"time"
)

func TestThrottleSpacesCalls(t *testing.T) {
	now := time.Unix(1000, 0)
	var slept []time.Duration
	th := &Throttle{
		Interval: 7 * time.Second,
		Now:      func() time.Time { return now },
		Sleep:    func(d time.Duration) { slept = append(slept, d); now = now.Add(d) },
	}
	th.Wait() // the first call does not wait
	now = now.Add(2 * time.Second)
	th.Wait() // 5 s left of the interval
	now = now.Add(10 * time.Second)
	th.Wait() // the interval has passed
	th.Wait() // right after the previous one: the whole interval
	want := []time.Duration{5 * time.Second, 7 * time.Second}
	if !reflect.DeepEqual(slept, want) {
		t.Errorf("slept %v, want %v", slept, want)
	}
}

func TestThrottleNilDoesNothing(t *testing.T) {
	var th *Throttle
	th.Wait()
}
