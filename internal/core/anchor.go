package core

import "fmt"

// AnchorKind says how a finding's line relates to the diff.
type AnchorKind string

// The anchor kinds.
const (
	// AnchorAdded, AnchorContext and AnchorRemoved anchor a finding on its
	// own line, which is in the diff.
	AnchorAdded   AnchorKind = "added"
	AnchorContext AnchorKind = "context"
	AnchorRemoved AnchorKind = "removed"
	// AnchorRelocated anchors a finding whose line is outside the hunks
	// on the nearest visible line of its file.
	AnchorRelocated AnchorKind = "relocated"
)

// Anchor is the line of the diff a finding is published on.
type Anchor struct {
	Kind AnchorKind `json:"kind"`
	Side Side       `json:"side"`
	Line int        `json:"line"`
	// RequestedLine is the line the finding is about, on the finding's
	// side; set only on a relocated anchor.
	RequestedLine int `json:"requestedLine,omitempty"`
}

// Placement is where a finding is published.
type Placement string

// The placements.
const (
	// Inline publishes a finding on the diff, at its anchor.
	Inline Placement = "inline"
	// Summary publishes a finding in the summary: its file shows no line.
	Summary Placement = "summary"
)

// Anchor decides where a finding about line on side is published. A line
// in the diff anchors there: added and context lines on the RIGHT side,
// removed lines on the LEFT side. A line outside the hunks is relocated to
// the nearest visible line on the same side, ties going to the lower line;
// when that side shows no line, the other side's lines are used. Anchor is
// nil when the diff shows no line at all.
func (d FileDiff) Anchor(side Side, line int) *Anchor {
	if _, l, ok := d.lineAt(side, line); ok {
		a := at(l)
		return &a
	}
	best, found := d.nearest(side, line)
	if !found {
		other := Right
		if side == Right {
			other = Left
		}
		if best, found = d.nearest(other, line); !found {
			return nil
		}
	}
	a := at(best)
	a.Kind, a.RequestedLine = AnchorRelocated, line
	return &a
}

// at anchors on a diff line: a removed line on the LEFT side, any other on
// the RIGHT side.
func at(l DiffLine) Anchor {
	switch l.Kind {
	case LineRemoved:
		return Anchor{Kind: AnchorRemoved, Side: Left, Line: l.Old}
	case LineAdded:
		return Anchor{Kind: AnchorAdded, Side: Right, Line: l.New}
	}
	return Anchor{Kind: AnchorContext, Side: Right, Line: l.New}
}

// nearest finds the visible line of the side whose number is closest to
// n, the lower one on a tie.
func (d FileDiff) nearest(side Side, n int) (DiffLine, bool) {
	var (
		best     DiffLine
		bestNum  int
		bestDist = -1
	)
	for _, h := range d.Hunks {
		for _, l := range h.Lines {
			num := l.New
			if side == Left {
				num = l.Old
			}
			if num == 0 {
				continue
			}
			dist := num - n
			if dist < 0 {
				dist = -dist
			}
			if bestDist < 0 || dist < bestDist || dist == bestDist && num < bestNum {
				best, bestNum, bestDist = l, num, dist
			}
		}
	}
	return best, bestDist >= 0
}

// Lead is the sentence the published text of a relocated finding starts
// with; it is empty for any other anchor.
func (a Anchor) Lead(path string) string {
	if a.Kind != AnchorRelocated {
		return ""
	}
	return fmt.Sprintf("Refers to line %d of `%s`, outside the changed lines.", a.RequestedLine, path)
}
