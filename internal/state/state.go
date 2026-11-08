// Package state keeps the state of one review run and moves it through its
// phases: resolved, collected, judged, finalized, planned and published.
// Phases only move forward by one step, except that planned may be
// repeated to regenerate the plan; published is terminal.
package state

import "fmt"

// Phase is the last phase a run completed.
type Phase string

// The phases, in order.
const (
	Resolved  Phase = "resolved"
	Collected Phase = "collected"
	Judged    Phase = "judged"
	Finalized Phase = "finalized"
	Planned   Phase = "planned"
	Published Phase = "published"
)

var phaseRank = map[Phase]int{Resolved: 1, Collected: 2, Judged: 3, Finalized: 4, Planned: 5, Published: 6}

// Valid reports whether p is a known phase.
func (p Phase) Valid() bool { return phaseRank[p] > 0 }

// MarshalText implements encoding.TextMarshaler.
func (p Phase) MarshalText() ([]byte, error) {
	if !p.Valid() {
		return nil, fmt.Errorf("unknown phase %q", string(p))
	}
	return []byte(p), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (p *Phase) UnmarshalText(b []byte) error {
	v := Phase(b)
	if !v.Valid() {
		return fmt.Errorf("unknown phase %q", string(b))
	}
	*p = v
	return nil
}

// TransitionError reports a phase change that is not allowed.
type TransitionError struct {
	From, To Phase
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("phase transition %s -> %s is not allowed", e.From, e.To)
}

// CheckTransition returns a *TransitionError unless a run may move from
// phase from to phase to.
func CheckTransition(from, to Phase) error {
	f, t := phaseRank[from], phaseRank[to]
	if f > 0 && (t == f+1 || from == Planned && to == Planned) {
		return nil
	}
	return &TransitionError{From: from, To: to}
}
