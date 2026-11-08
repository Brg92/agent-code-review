package state

import (
	"errors"
	"fmt"
	"slices"

	"github.com/Brg92/agent-code-review/internal/core"
)

// validateCascade checks the shape of the change request list. The list is
// the depth-first walk of the cascade in discovery order, so:
//
//   - the first entry is the root, reviewed, at depth 0, without a parent;
//   - every other entry has a parent and a depth of at least 1, and its
//     parent is the reviewed entry that was open at depth-1 when it was
//     found; skipped entries are leaves;
//   - a change request is reviewed at most once, and the same key may
//     appear again only as a skipped entry;
//   - a cycle names a change request reviewed earlier, a self-reference
//     names its own parent, other-platform leaves the platform of the
//     cascade, and every reviewed entry is on the platform of the cascade;
//   - at most maxChangeRequests entries are reviewed, none deeper than
//     maxDepth; over-depth-limit and over-count-limit are used only where
//     their limit applies, depth first.
func (s *State) validateCascade() error {
	crs := s.ChangeRequests
	if len(crs) == 0 {
		return &ConsistencyError{Index: -1, Problem: "no change requests: the first must be the root"}
	}
	fail := func(i int, format string, args ...any) error {
		return &ConsistencyError{Index: i, CR: crs[i].CR, Problem: fmt.Sprintf(format, args...)}
	}
	root := crs[0]
	switch {
	case root.CR != s.Root:
		return fail(0, "the first change request must be the root %s", s.Root)
	case root.Status == core.Skipped:
		return fail(0, "the root cannot be skipped")
	case root.Parent != nil || root.Depth != 0:
		return fail(0, "the root has no parent and is at depth 0")
	}
	// open holds the reviewed entries on the current path, one per depth.
	open := []core.Entry{root}
	reviewed := []core.Key{root.CR}
	for i := 1; i < len(crs); i++ {
		e := crs[i]
		if e.Parent == nil {
			return fail(i, "no parent")
		}
		if e.Depth < 1 {
			return fail(i, "depth %d is reserved for the root", e.Depth)
		}
		if e.Depth > len(open) {
			return fail(i, "depth %d has no parent among the earlier change requests", e.Depth)
		}
		open = open[:e.Depth]
		if parent := open[e.Depth-1]; !parent.CR.Same(*e.Parent) {
			return fail(i, "parent %s is not the open change request %s at depth %d", *e.Parent, parent.CR, e.Depth-1)
		}
		if e.Status == core.Skipped {
			if err := s.checkSkipped(e, len(reviewed), reviewed); err != nil {
				return fail(i, "%v", err)
			}
			continue
		}
		switch {
		case slices.ContainsFunc(reviewed, e.CR.Same):
			return fail(i, "reviewed twice")
		case !s.onPlatform(e.CR):
			return fail(i, "not on the platform of the cascade (%s:%s)", s.Forge, s.Root.Host)
		case e.Depth > s.Cascade.MaxDepth:
			return fail(i, "depth %d is deeper than maxDepth %d", e.Depth, s.Cascade.MaxDepth)
		case len(reviewed) >= s.Cascade.MaxChangeRequests:
			return fail(i, "more than maxChangeRequests %d change requests reviewed", s.Cascade.MaxChangeRequests)
		}
		reviewed = append(reviewed, e.CR)
		open = append(open, e)
	}
	return nil
}

func (s *State) onPlatform(k core.Key) bool { return k.Forge == s.Forge && k.Host == s.Root.Host }

// checkSkipped checks the skip reason of e against what is known when e is
// found: count change requests are already reviewed.
func (s *State) checkSkipped(e core.Entry, count int, reviewed []core.Key) error {
	switch e.SkipReason {
	case core.SelfReference:
		if !e.CR.Same(*e.Parent) {
			return errors.New("self-reference to another change request than its parent")
		}
	case core.Cycle:
		if !slices.ContainsFunc(reviewed, e.CR.Same) {
			return errors.New("cycle on a change request that is not reviewed earlier")
		}
	case core.OtherPlatform:
		if s.onPlatform(e.CR) {
			return errors.New("other-platform on the platform of the cascade")
		}
	case core.OverDepthLimit:
		if e.Depth <= s.Cascade.MaxDepth {
			return fmt.Errorf("over-depth-limit at depth %d, within maxDepth %d", e.Depth, s.Cascade.MaxDepth)
		}
	case core.OverCountLimit:
		if e.Depth > s.Cascade.MaxDepth {
			return fmt.Errorf("over-count-limit at depth %d, beyond maxDepth %d: it is over-depth-limit", e.Depth, s.Cascade.MaxDepth)
		}
		if count < s.Cascade.MaxChangeRequests {
			return fmt.Errorf("over-count-limit with %d reviewed, below maxChangeRequests %d", count, s.Cascade.MaxChangeRequests)
		}
	}
	return nil
}
