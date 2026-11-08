package refs

import (
	"regexp"
	"slices"
	"strings"

	"github.com/Brg92/agent-code-review/internal/core"
)

// Box is the state of the checkbox at the start of a Related Changes line.
type Box int

// The checkbox states.
const (
	// NoBox is a line without a checkbox.
	NoBox Box = iota
	// Open is "[ ]".
	Open
	// Ticked is "[x]" or "[X]": the review of that change request is done.
	Ticked
)

// Kind says what the cascade does with a line.
type Kind int

// The kinds of line that are not free prose.
const (
	// Followed has a checkbox and at least one reference.
	Followed Kind = iota + 1
	// NotFollowed has references but no checkbox: it is reported, not
	// followed, and does not make the cascade incomplete.
	NotFollowed
	// Unparsed has a checkbox but no recognisable reference: it is
	// reported and makes the cascade incomplete.
	Unparsed
)

func (k Kind) String() string {
	switch k {
	case Followed:
		return "followed"
	case NotFollowed:
		return "notfollowed"
	case Unparsed:
		return "unparsed"
	}
	return "unknown"
}

// RelatedEntry is one line of the Related Changes section that has a
// checkbox or a reference. Free prose is not an entry.
type RelatedEntry struct {
	// Line is the 1-based line number in the description.
	Line int
	// Text is the line as written, without its line ending.
	Text string
	Box  Box
	// BoxOffset is the byte offset in the description of the character
	// inside the brackets of the checkbox, or -1 without one.
	BoxOffset int
	// Refs are the change requests on the line, in line order, each once.
	Refs []core.Key
}

// Kind classifies the line.
func (e RelatedEntry) Kind() Kind {
	switch {
	case len(e.Refs) == 0:
		return Unparsed
	case e.Box == NoBox:
		return NotFollowed
	}
	return Followed
}

var (
	// headingRe matches the heading of a Related Changes section.
	headingRe = regexp.MustCompile(`(?i)^ {0,3}#{1,3}[ \t]*Related[ \t]+(?:Changes|Merge[ \t]+Requests|MRs|Pull[ \t]+Requests|PRs)[ \t]*:?[ \t]*#*[ \t]*$`)
	// anyHeadingRe matches a heading of level 1 to 3, which ends a section.
	anyHeadingRe = regexp.MustCompile(`^ {0,3}#{1,3}(?:[ \t]|$)`)
	fenceOpenRe  = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
	boxRe        = regexp.MustCompile(`^[ \t]*(?:(?:[-*+]|[0-9]+[.)])[ \t]+)?\[([ xX])\]`)
)

// Related returns the entries of the Related Changes sections of a
// description, in line order. The heading is level 1 to 3 and names the
// section "Related Changes", "Related Merge Requests", "Related MRs",
// "Related Pull Requests" or "Related PRs" in any case; the section ends at
// the next heading of level 1 to 3. Lines inside fenced code blocks and
// HTML comments are ignored. Lines end at "\n", with an optional "\r" before
// it. from is the change request that owns the description.
func Related(description string, reg Registry, from core.Key) []RelatedEntry {
	var entries []RelatedEntry
	var fence string // the opening fence, while inside a fenced block
	inComment, inSection := false, false
	offset := 0
	for n, raw := range strings.Split(description, "\n") {
		lineStart := offset
		offset += len(raw) + 1
		line := strings.TrimSuffix(raw, "\r")

		if fence != "" {
			if closesFence(line, fence) {
				fence = ""
			}
			continue
		}
		if inComment {
			inComment = !strings.Contains(line, "-->")
			continue
		}
		if m := fenceOpenRe.FindStringSubmatch(line); m != nil {
			fence = m[1]
			continue
		}
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "<!--") {
			inComment = !strings.Contains(t, "-->")
			continue
		}
		if !inSection {
			inSection = headingRe.MatchString(line)
			continue
		}
		if anyHeadingRe.MatchString(line) {
			inSection = headingRe.MatchString(line)
			continue
		}

		e := RelatedEntry{Line: n + 1, Text: line, BoxOffset: -1}
		if m := boxRe.FindStringSubmatchIndex(line); m != nil {
			e.Box, e.BoxOffset = Open, lineStart+m[2]
			if line[m[2]] != ' ' {
				e.Box = Ticked
			}
		}
		e.Refs = lineRefs(reg.Parse(line, from))
		if e.Box != NoBox || len(e.Refs) > 0 {
			entries = append(entries, e)
		}
	}
	return entries
}

// closesFence reports whether line closes a fenced block opened by fence:
// the same character, at least as many, and nothing else.
func closesFence(line, fence string) bool {
	t := strings.TrimLeft(line, " ")
	if len(line)-len(t) > 3 {
		return false
	}
	t = strings.TrimRight(t, " \t")
	return len(t) >= len(fence) && strings.Trim(t, fence[:1]) == ""
}

// lineRefs orders references by position and keeps the first of each key.
func lineRefs(found []Ref) []core.Key {
	slices.SortStableFunc(found, func(a, b Ref) int { return a.Pos - b.Pos })
	var keys []core.Key
	for _, r := range found {
		if !slices.ContainsFunc(keys, r.Key.Same) {
			keys = append(keys, r.Key)
		}
	}
	return keys
}
