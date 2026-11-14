package core

import (
	"fmt"
	"regexp"
	"strings"
)

// RepositorySource is the source of a rule a repository writes in its
// criteria files, as opposed to the rules of the packs and the core.
const RepositorySource = "repository"

// RepositoryRule is a rule of a repository: a list item of the form
// - `<id>` — **<SEVERITY>**. <text> in one of its criteria files.
type RepositoryRule struct {
	ID string `json:"id"`
	// Source is always RepositorySource.
	Source   string   `json:"source"`
	Severity Severity `json:"severity"`
	Text     string   `json:"text"`
	// File is the criteria file, relative to the repository root; Line is
	// the 1-based line of the list item.
	File string `json:"file"`
	Line int    `json:"line"`
}

// IgnoredPrefix starts every note about a part of the repository criteria
// that is not applied.
const IgnoredPrefix = "Repository criteria ignored: "

// IgnoredItem is a part of the repository criteria that is not applied:
// what it is and where, and why.
type IgnoredItem struct {
	Item   string `json:"item"`
	Reason string `json:"reason"`
}

// Note is the note of the summary about the item.
func (i IgnoredItem) Note() string { return IgnoredPrefix + i.Item + " — " + i.Reason }

var (
	// ruleItem is a list item that starts with a backticked id, a
	// separator and a bold severity; the text follows.
	ruleItem = regexp.MustCompile("^ {0,3}[-*+]\\s+`([^`]+)`\\s*(?:—|–|-|,)?\\s*\\*\\*([^*]+)\\*\\*\\.?\\s*(.*)$")
	// idItem is a list item that starts with a backticked id and a
	// separator but has no bold severity.
	idItem   = regexp.MustCompile("^ {0,3}[-*+]\\s+`([^`]+)`\\s*(?:—|–|-|,)")
	listItem = regexp.MustCompile(`^ {0,3}[-*+]\s+\S`)
	// backticked is a code span of a line.
	backticked = regexp.MustCompile("`([^`]+)`")
	heading    = regexp.MustCompile(`^ {0,3}(#{1,6})\s+(.+?)\s*#*\s*$`)
	fence      = regexp.MustCompile("^ {0,3}(```|~~~)")
	// continuation is a line that goes on with the text of a list item.
	continuation  = regexp.MustCompile(`^\s+\S`)
	kebabCase     = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	forbiddenHead = regexp.MustCompile(`(?i)^(baseline\s+suppressions|severity\s+changes)\b`)
	exceptionHead = regexp.MustCompile(`(?i)^known\s+exceptions\b`)
	rulesHead     = regexp.MustCompile(`(?i)\brules\b`)
)

// section kinds of a criteria file.
const (
	sectionOther = iota
	sectionRules
	sectionExceptions
	sectionForbidden
)

// ruleReader collects the repository rules of the criteria files of one
// layout, in file order, and each part it ignores.
type ruleReader struct {
	// reserved are the ids of the pack and core rules.
	reserved map[string]bool
	// first is where each id was defined first.
	first   map[string]string
	rules   []RepositoryRule
	ignored []IgnoredItem
}

// notes are the notes of the parts the reader ignored.
func (r *ruleReader) notes() []string {
	var out []string
	for _, i := range r.ignored {
		out = append(out, i.Note())
	}
	return out
}

func newRuleReader(reserved map[string]bool) *ruleReader {
	return &ruleReader{reserved: reserved, first: map[string]string{}}
}

func (r *ruleReader) ignore(item, at, reason string) {
	r.ignored = append(r.ignored, IgnoredItem{Item: item + " (" + at + ")", Reason: reason})
}

// read reads the rules of one criteria file. Front matter and code fences
// are skipped. A rule-form list item is a rule wherever it is, except in a
// forbidden section; under a rules heading, any other list item is
// reported. A heading is a rules heading when its title has the word
// rules, or, with everySection set (the references files of the legacy
// layout), when it is below the title; its subsections are rules sections
// too. A known exception, a list item under a Known exceptions heading,
// that names a pack or core rule is reported.
func (r *ruleReader) read(file, content string, everySection bool) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	type open struct{ level, kind int }
	var stack []open
	kind := sectionOther
	inFence, start := false, 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				start = i + 1
				break
			}
		}
	}
	for i := start; i < len(lines); i++ {
		line, n := lines[i], i+1
		at := fmt.Sprintf("%s:%d", file, n)
		if fence.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if m := heading.FindStringSubmatch(line); m != nil {
			level, title := len(m[1]), m[2]
			for len(stack) > 0 && stack[len(stack)-1].level >= level {
				stack = stack[:len(stack)-1]
			}
			parent := sectionOther
			if len(stack) > 0 {
				parent = stack[len(stack)-1].kind
			}
			switch {
			case forbiddenHead.MatchString(title):
				kind = sectionForbidden
				r.ignore(fmt.Sprintf("section %q", title), at, "pack and core rules cannot be switched off or re-weighted")
			case exceptionHead.MatchString(title):
				kind = sectionExceptions
			case parent == sectionForbidden || parent == sectionRules || parent == sectionExceptions:
				kind = parent
			case rulesHead.MatchString(title) || everySection && level > 1:
				kind = sectionRules
			default:
				kind = sectionOther
			}
			stack = append(stack, open{level, kind})
			continue
		}
		if kind == sectionForbidden {
			continue
		}
		if m := ruleItem.FindStringSubmatch(line); m != nil {
			text := []string{strings.TrimSpace(m[3])}
			for i+1 < len(lines) && continuation.MatchString(lines[i+1]) && !listItem.MatchString(lines[i+1]) {
				i++
				text = append(text, strings.TrimSpace(lines[i]))
			}
			r.add(m[1], strings.TrimSpace(m[2]), strings.TrimSpace(strings.Join(text, " ")), file, n)
			continue
		}
		if kind == sectionExceptions && listItem.MatchString(line) {
			r.exception(line, at)
			continue
		}
		if kind != sectionRules || !listItem.MatchString(line) {
			continue
		}
		if m := idItem.FindStringSubmatch(line); m != nil {
			r.ignore("`"+m[1]+"`", at, "it has no **<SEVERITY>**, so it cannot become a finding (warning)")
			continue
		}
		r.ignore("list item", at, "it has no rule id, so it cannot become a finding (warning)")
	}
}

// exception reports each pack or core rule a known exception names, once
// per item: a repository may declare exceptions to its own rules only.
func (r *ruleReader) exception(line, at string) {
	seen := map[string]bool{}
	for _, m := range backticked.FindAllStringSubmatch(line, -1) {
		if id := strings.TrimSpace(m[1]); r.reserved[id] && !seen[id] {
			seen[id] = true
			r.ignore("known exception for `"+id+"`", at, "pack and core rules always apply")
		}
	}
}

// add checks one rule-form item, at a 1-based line of file, and keeps it
// as a rule.
func (r *ruleReader) add(id, severity, text, file string, line int) {
	item, at := "`"+id+"`", fmt.Sprintf("%s:%d", file, line)
	switch s := Severity(severity); {
	case !kebabCase.MatchString(id):
		r.ignore(item, at, "the id is not kebab-case")
	case s != Blocker && s != Major && s != Minor:
		r.ignore(item, at, fmt.Sprintf("severity %s is not BLOCKER, MAJOR or MINOR", severity))
	case r.reserved[id]:
		r.ignore(item, at, "the id is already used by a pack or core rule")
	case r.first[id] != "":
		r.ignore(item, at, "duplicate id, first defined at "+r.first[id])
	case text == "":
		r.ignore(item, at, "the rule has no text")
	default:
		r.first[id] = at
		r.rules = append(r.rules, RepositoryRule{ID: id, Source: RepositorySource, Severity: s, Text: text, File: file, Line: line})
	}
}
