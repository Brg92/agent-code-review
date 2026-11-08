package core

import (
	"errors"
	"fmt"
	"regexp"
)

// Limits bound a cascade.
type Limits struct {
	// MaxDepth is the deepest level of Related Changes that is reviewed;
	// the root is level 0.
	MaxDepth int `json:"maxDepth"`
	// MaxChangeRequests is the most change requests reviewed in one
	// cascade, the root included, counted in discovery order.
	MaxChangeRequests int `json:"maxChangeRequests"`
}

// DefaultLimits returns the limits used when a repository sets none.
func DefaultLimits() Limits { return Limits{MaxDepth: 3, MaxChangeRequests: 10} }

// Validate reports limits that cannot bound a cascade.
func (l Limits) Validate() error {
	if l.MaxDepth < 0 {
		return fmt.Errorf("maxDepth %d is negative", l.MaxDepth)
	}
	if l.MaxChangeRequests < 1 {
		return fmt.Errorf("maxChangeRequests %d is less than 1", l.MaxChangeRequests)
	}
	return nil
}

// Status is the progress of one change request of a cascade.
type Status string

// The statuses. Pending, Collected, Judged and Finalized are ordered;
// Skipped is outside that order.
const (
	Pending   Status = "pending"
	Collected Status = "collected"
	Judged    Status = "judged"
	Finalized Status = "finalized"
	Skipped   Status = "skipped"
)

var statusRank = map[Status]int{Pending: 1, Collected: 2, Judged: 3, Finalized: 4}

// Valid reports whether s is a known status.
func (s Status) Valid() bool { return s == Skipped || statusRank[s] > 0 }

// AtLeast reports whether s is other or a later status. It is false when
// either is Skipped or unknown.
func (s Status) AtLeast(other Status) bool {
	a, b := statusRank[s], statusRank[other]
	return a > 0 && b > 0 && a >= b
}

// MarshalText implements encoding.TextMarshaler.
func (s Status) MarshalText() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("unknown status %q", string(s))
	}
	return []byte(s), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (s *Status) UnmarshalText(b []byte) error {
	v := Status(b)
	if !v.Valid() {
		return fmt.Errorf("unknown status %q", string(b))
	}
	*s = v
	return nil
}

// SkipReason says why a cascade entry is not reviewed.
type SkipReason string

// The skip reasons. OverDepthLimit, OverCountLimit, OtherPlatform and
// NotFound make the cascade incomplete; the others do not.
const (
	AlreadyDone       SkipReason = "already-done"
	NotAChangeRequest SkipReason = "not-a-change-request"
	NotFound          SkipReason = "not-found"
	OtherPlatform     SkipReason = "other-platform"
	OverDepthLimit    SkipReason = "over-depth-limit"
	OverCountLimit    SkipReason = "over-count-limit"
	Cycle             SkipReason = "cycle"
	SelfReference     SkipReason = "self-reference"
)

// Valid reports whether r is a known skip reason.
func (r SkipReason) Valid() bool {
	switch r {
	case AlreadyDone, NotAChangeRequest, NotFound, OtherPlatform, OverDepthLimit, OverCountLimit, Cycle, SelfReference:
		return true
	}
	return false
}

// MarshalText implements encoding.TextMarshaler.
func (r SkipReason) MarshalText() ([]byte, error) {
	if !r.Valid() {
		return nil, fmt.Errorf("unknown skip reason %q", string(r))
	}
	return []byte(r), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (r *SkipReason) UnmarshalText(b []byte) error {
	v := SkipReason(b)
	if !v.Valid() {
		return fmt.Errorf("unknown skip reason %q", string(b))
	}
	*r = v
	return nil
}

// Entry is one change request of a cascade: the root or a Related Changes
// entry found from it, skipped or not.
type Entry struct {
	CR    Key `json:"cr"`
	Depth int `json:"depth"`
	// Parent listed this entry in its Related Changes; nil for the root.
	Parent     *Key       `json:"parent,omitempty"`
	Status     Status     `json:"status"`
	SkipReason SkipReason `json:"skipReason,omitempty"`
	// HeadSHA is the head commit, once collected.
	HeadSHA string `json:"headSha,omitempty"`
	// Title is the title on the platform, once known.
	Title string `json:"title,omitempty"`
}

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Validate reports an entry that breaks the rules of a single entry. A
// skipped entry always has a skip reason, and only skipped entries do.
func (e Entry) Validate() error {
	if e.CR.IsZero() {
		return errors.New("missing change request key")
	}
	if e.Depth < 0 {
		return fmt.Errorf("depth %d is negative", e.Depth)
	}
	if e.Parent != nil && e.Parent.IsZero() {
		return errors.New("empty parent key")
	}
	if !e.Status.Valid() {
		return fmt.Errorf("unknown status %q", string(e.Status))
	}
	switch {
	case e.Status == Skipped && e.SkipReason == "":
		return errors.New("skipped entry without skipReason")
	case e.Status != Skipped && e.SkipReason != "":
		return fmt.Errorf("skipReason %q on a %s entry", string(e.SkipReason), e.Status)
	case e.SkipReason != "" && !e.SkipReason.Valid():
		return fmt.Errorf("unknown skipReason %q", string(e.SkipReason))
	}
	if e.HeadSHA != "" && !shaPattern.MatchString(e.HeadSHA) {
		return fmt.Errorf("headSha %q is not 40 lowercase hexadecimal characters", e.HeadSHA)
	}
	return nil
}
