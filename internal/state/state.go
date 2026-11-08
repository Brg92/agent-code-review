// Package state keeps the state of one review run and moves it through its
// phases: resolved, collected, judged, finalized, planned and published.
// Phases only move forward by one step, except that planned may be
// repeated to regenerate the plan; published is terminal.
package state

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/Brg92/agent-code-review/internal/core"
)

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

// State is the state of one review run.
type State struct {
	RunID string
	// Forge is the platform of the whole cascade, the root's forge.
	Forge core.Forge
	// Root is the change request the review started from.
	Root  core.Key
	Phase Phase
	// Cascade holds the limits actually used in this run.
	Cascade   core.Limits
	CreatedAt time.Time
	UpdatedAt time.Time
	// ChangeRequests lists the root and every Related Changes entry found
	// from it, skipped ones included, in discovery order.
	ChangeRequests []core.Entry
}

// ConsistencyError reports a state that breaks its rules.
type ConsistencyError struct {
	// Index is the position of the offending change request, or -1 when
	// the problem is not about one change request.
	Index   int
	CR      core.Key
	Problem string
}

func (e *ConsistencyError) Error() string {
	if e.Index < 0 {
		return "inconsistent state: " + e.Problem
	}
	return fmt.Sprintf("inconsistent state: change request %d (%s): %s", e.Index, e.CR, e.Problem)
}

// New starts a run in phase resolved. The forge is the root's; entries are
// copied. now is stored in UTC, to the second.
func New(runID string, root core.Key, limits core.Limits, entries []core.Entry, now time.Time) (*State, error) {
	now = stamp(now)
	s := &State{
		RunID:          runID,
		Forge:          root.Forge,
		Root:           root,
		Phase:          Resolved,
		Cascade:        limits,
		CreatedAt:      now,
		UpdatedAt:      now,
		ChangeRequests: slices.Clone(entries),
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return s, nil
}

func stamp(t time.Time) time.Time { return t.UTC().Truncate(time.Second) }

// Advance moves the run to phase to and sets UpdatedAt to now. It returns a
// *TransitionError for a transition that is not allowed and a
// *ConsistencyError when the change requests are not ready for the new
// phase; in both cases the state is unchanged.
func (s *State) Advance(to Phase, now time.Time) error {
	if err := CheckTransition(s.Phase, to); err != nil {
		return err
	}
	next := *s
	next.Phase = to
	if err := next.Validate(); err != nil {
		return err
	}
	s.Phase = to
	s.UpdatedAt = stamp(now)
	return nil
}

// requiredStatus is the least status every non-skipped change request must
// have in a phase; empty when there is none.
func requiredStatus(p Phase) core.Status {
	switch p {
	case Collected:
		return core.Collected
	case Judged:
		return core.Judged
	case Finalized, Planned, Published:
		return core.Finalized
	}
	return ""
}

// Validate returns a *ConsistencyError when the state breaks its rules:
// valid fields, valid entries, and every non-skipped change request at
// least as far as the phase requires.
func (s *State) Validate() error {
	run := func(err error) error { return &ConsistencyError{Index: -1, Problem: err.Error()} }
	switch {
	case !s.Forge.Valid():
		return run(fmt.Errorf("unknown forge %q", string(s.Forge)))
	case s.Root.IsZero():
		return run(errors.New("missing root"))
	case s.Root.Forge != s.Forge:
		return run(fmt.Errorf("root %s is not on forge %s", s.Root, s.Forge))
	case !s.Phase.Valid():
		return run(fmt.Errorf("unknown phase %q", string(s.Phase)))
	case s.CreatedAt.IsZero():
		return run(errors.New("missing createdAt"))
	case s.UpdatedAt.IsZero():
		return run(errors.New("missing updatedAt"))
	}
	if err := s.Cascade.Validate(); err != nil {
		return run(err)
	}
	want := requiredStatus(s.Phase)
	for i, e := range s.ChangeRequests {
		entry := func(err error) error { return &ConsistencyError{Index: i, CR: e.CR, Problem: err.Error()} }
		if err := e.Validate(); err != nil {
			return entry(err)
		}
		if want != "" && e.Status != core.Skipped && !e.Status.AtLeast(want) {
			return entry(fmt.Errorf("status %s, but phase %s needs at least %s", e.Status, s.Phase, want))
		}
	}
	return nil
}
