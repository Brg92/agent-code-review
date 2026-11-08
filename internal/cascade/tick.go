package cascade

import (
	"fmt"

	"github.com/Brg92/agent-code-review/internal/refs"
)

// TickReason says why a checkbox cannot be ticked.
type TickReason int

// The reasons.
const (
	// AlreadyTicked: the checkbox is already "[x]".
	AlreadyTicked TickReason = iota + 1
	// NoCheckbox: the entry has no checkbox.
	NoCheckbox
	// Moved: "[ ]" is not at the offset of the entry any more, because the
	// description changed since the entry was read.
	Moved
)

// TickError reports a checkbox that cannot be ticked.
type TickError struct {
	Reason TickReason
	// Line is the line number of the entry.
	Line int
}

func (e *TickError) Error() string {
	what := "the description changed since it was read: the checkbox moved"
	switch e.Reason {
	case AlreadyTicked:
		what = "the checkbox is already ticked"
	case NoCheckbox:
		what = "the line has no checkbox"
	}
	return fmt.Sprintf("cannot tick line %d: %s", e.Line, what)
}

// Tick returns description with the checkbox of entry turned from "[ ]"
// into "[x]". It changes exactly one byte and keeps every other byte,
// line endings included. The entry must come from refs.Related on this
// description: if "[ ]" is not where the entry says, or is already ticked,
// Tick returns a *TickError. It does not decide whether the entry should
// be ticked.
func Tick(description string, entry refs.RelatedEntry) (string, error) {
	fail := func(r TickReason) (string, error) { return "", &TickError{Reason: r, Line: entry.Line} }
	switch entry.Box {
	case refs.NoBox:
		return fail(NoCheckbox)
	case refs.Ticked:
		return fail(AlreadyTicked)
	}
	i := entry.BoxOffset
	if i < 1 || i+2 > len(description) {
		return fail(Moved)
	}
	switch description[i-1 : i+2] {
	case "[ ]":
		return description[:i] + "x" + description[i+1:], nil
	case "[x]", "[X]":
		return fail(AlreadyTicked)
	}
	return fail(Moved)
}
