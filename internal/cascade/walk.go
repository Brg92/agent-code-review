// Package cascade resolves the cascade of a change request: it walks the
// Related Changes sections from the root, depth first, and produces the
// entries of the state, the order of the review, the causes that make the
// cascade INCOMPLETE and the lines that were not followed. It reads change
// requests through a Source and knows no platform.
package cascade

import (
	"context"
	"fmt"
	"strings"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/refs"
)

// Kind says what a key names on the platform.
type Kind int

// The kinds. A number that names neither is KindNotFound, which also
// stands for a change request the token cannot read (404 or 403).
const (
	KindNotFound Kind = iota
	KindChangeRequest
	KindIssue
)

func (k Kind) String() string {
	switch k {
	case KindChangeRequest:
		return "change request"
	case KindIssue:
		return "issue"
	}
	return "nothing"
}

// Item is what a Source knows about a key.
type Item struct {
	Kind Kind
	// Title, Description and Head are set for a change request: the title,
	// the description as written and the head commit SHA.
	Title       string
	Description string
	Head        string
}

// Source reads change requests from a platform. A change request that does
// not exist or cannot be read is not an error: it is an Item of KindNotFound.
// An error is a failure to ask.
type Source interface {
	Get(ctx context.Context, key core.Key) (Item, error)
}

// Line is a line of a Related Changes section that is reported.
type Line struct {
	// CR is the change request whose description holds the line.
	CR core.Key
	// Line is the 1-based line number in its description.
	Line int
	Text string
}

// Cause is a reason for the cascade to be INCOMPLETE.
type Cause struct {
	// Reason is a core.SkipReason that makes a cascade incomplete, or
	// "unparsed".
	Reason string
	// CR is the change request that was skipped; zero for an unparsed line.
	CR core.Key
	// Parent is the change request whose description holds the line.
	Parent core.Key
	// Line is the 1-based line number of an unparsed line; 0 otherwise.
	Line int
}

// Unparsed is the Reason of the cause of a line with a checkbox and no
// reference.
const Unparsed = "unparsed"

// Result is a resolved cascade.
type Result struct {
	// Entries are the root and every entry found from it, skipped ones
	// included, in discovery order (depth first, in line order). They are
	// the change requests of the state.
	Entries []core.Entry
	// Order is the order of the review: related change requests first,
	// the root last.
	Order []core.Key
	// NotFollowed are the lines with a reference and no checkbox. They are
	// reported, not followed, and do not make the cascade incomplete.
	NotFollowed []Line
	// Unparsed are the lines with a checkbox and no reference. They make
	// the cascade incomplete.
	Unparsed []Line
}

// Causes lists why the cascade is incomplete: the skipped entries whose
// reason says so, in discovery order, then the unparsed lines.
func (r Result) Causes() []Cause {
	var causes []Cause
	for _, e := range r.Entries {
		if e.Status == core.Skipped && e.SkipReason.Incomplete() {
			causes = append(causes, Cause{Reason: string(e.SkipReason), CR: e.CR, Parent: *e.Parent})
		}
	}
	for _, l := range r.Unparsed {
		causes = append(causes, Cause{Reason: Unparsed, Parent: l.CR, Line: l.Line})
	}
	return causes
}

// Incomplete reports whether part of the cascade was not reviewed.
func (r Result) Incomplete() bool { return len(r.Causes()) > 0 }

// RootError reports a root that is not a change request.
type RootError struct {
	Key  core.Key
	Kind Kind
}

func (e *RootError) Error() string {
	return fmt.Sprintf("%s is not a change request: it names %s", e.Key, e.Kind)
}

// Resolve walks the cascade of root. reg must belong to the forge of root;
// links of other platforms and of other hosts are recognised only to be
// skipped (see refs.Compose). An error from src ends the walk; a change
// request that is not found, is an issue or is beyond a limit is a skipped
// entry of the result, not an error.
func Resolve(ctx context.Context, src Source, reg refs.Registry, root core.Key, limits core.Limits) (Result, error) {
	if err := limits.Validate(); err != nil {
		return Result{}, err
	}
	if reg.Forge() != root.Forge {
		return Result{}, fmt.Errorf("registry of %s cannot read the cascade of %s", reg.Forge(), root)
	}
	w := &walker{ctx: ctx, src: src, reg: reg, root: root, limits: limits, cache: map[string]Item{}}
	item, err := w.get(root)
	if err != nil {
		return Result{}, err
	}
	if item.Kind != KindChangeRequest {
		return Result{}, &RootError{Key: root, Kind: item.Kind}
	}
	if err := w.review(core.Entry{CR: root}, item); err != nil {
		return Result{}, err
	}
	return w.res, nil
}

type walker struct {
	ctx    context.Context
	src    Source
	reg    refs.Registry
	root   core.Key
	limits core.Limits
	res    Result
	// reviewed are the keys of the entries that are reviewed so far.
	reviewed []core.Key
	cache    map[string]Item
}

func (w *walker) get(k core.Key) (Item, error) {
	id := strings.ToLower(k.String())
	if it, ok := w.cache[id]; ok {
		return it, nil
	}
	if err := w.ctx.Err(); err != nil {
		return Item{}, err
	}
	it, err := w.src.Get(w.ctx, k)
	if err != nil {
		return Item{}, fmt.Errorf("read %s: %w", k, err)
	}
	w.cache[id] = it
	return it, nil
}

// review records e as a reviewed entry, walks its Related Changes and puts
// it in the review order after everything it relates to.
func (w *walker) review(e core.Entry, item Item) error {
	e.Status, e.Title, e.HeadSHA = core.Pending, item.Title, item.Head
	if err := e.Validate(); err != nil {
		return fmt.Errorf("read %s: %w", e.CR, err)
	}
	w.res.Entries = append(w.res.Entries, e)
	w.reviewed = append(w.reviewed, e.CR)
	for _, line := range refs.Related(item.Description, w.reg, e.CR) {
		l := Line{CR: e.CR, Line: line.Line, Text: line.Text}
		switch line.Kind() {
		case refs.Unparsed:
			w.res.Unparsed = append(w.res.Unparsed, l)
		case refs.NotFollowed:
			w.res.NotFollowed = append(w.res.NotFollowed, l)
		default:
			for _, key := range line.Refs {
				if err := w.follow(e, key, line.Box == refs.Ticked); err != nil {
					return err
				}
			}
		}
	}
	w.res.Order = append(w.res.Order, e.CR)
	return nil
}

// follow decides what to do with a reference found in parent: skip it, or
// review it. The checks go from the cheapest, which need no request, to the
// limits, which need to know that the reference is a change request.
func (w *walker) follow(parent core.Entry, key core.Key, ticked bool) error {
	e := core.Entry{CR: key, Depth: parent.Depth + 1, Parent: &parent.CR}
	skip := func(reason core.SkipReason) error {
		e.Status, e.SkipReason = core.Skipped, reason
		w.res.Entries = append(w.res.Entries, e)
		return nil
	}
	switch {
	case ticked:
		return skip(core.AlreadyDone)
	case key.Forge != w.root.Forge || key.Host != w.root.Host:
		return skip(core.OtherPlatform)
	case key.Same(parent.CR):
		return skip(core.SelfReference)
	case w.isReviewed(key):
		return skip(core.Cycle)
	}
	item, err := w.get(key)
	if err != nil {
		return err
	}
	switch {
	case item.Kind == KindNotFound:
		return skip(core.NotFound)
	case item.Kind == KindIssue:
		return skip(core.NotAChangeRequest)
	case e.Depth > w.limits.MaxDepth:
		return skip(core.OverDepthLimit)
	case len(w.reviewed) >= w.limits.MaxChangeRequests:
		return skip(core.OverCountLimit)
	}
	return w.review(e, item)
}

func (w *walker) isReviewed(k core.Key) bool {
	for _, r := range w.reviewed {
		if r.Same(k) {
			return true
		}
	}
	return false
}
