package core

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	relativePath = regexp.MustCompile(`^(?:[^/\\.][^/\\]*|\.[^/\\.][^/\\]*)(?:/(?:[^/\\.][^/\\]*|\.[^/\\.][^/\\]*))*$`)
	projectKey   = regexp.MustCompile(`^(?:github|gitlab):[a-z0-9.-]+(?::[0-9]+)?/[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)+$`)
	noSpace      = regexp.MustCompile(`^\S+$`)
)

// ValidateJudgement checks a judgement against the change request it
// judges and returns a ValidationError with every problem, or nil. It
// checks that the judgement is for the change request and head of the
// review; that each finding applies a rule of the review, is on a changed
// file and on lines of that file's side, and says what is wrong, why and
// how to fix it; that only a pre-existing BLOCKER is a crash; that every
// signal that needs judgement is decided, each signal at most once and
// none unknown; and that the notes are within their limits.
func ValidateJudgement(r Review, j Judgement) error {
	v := &validator{review: r}
	v.check(j.Schema == 1, "/schema", "schema is %d, want 1", j.Schema)
	if cr, err := ParseKey(j.CR); err != nil {
		v.add("/cr", "%v", err)
	} else {
		v.check(cr.Same(r.CR), "/cr", "judges %s, the work package is for %s", j.CR, r.CR)
	}
	v.check(j.HeadSHA == r.HeadSHA, "/headSha", "judges head %s, the work package is for head %s", j.HeadSHA, r.HeadSHA)
	for i, f := range j.Findings {
		v.finding(fmt.Sprintf("/findings/%d", i), f, false)
	}
	for i, f := range j.PreExisting {
		v.finding(fmt.Sprintf("/preExisting/%d", i), f, true)
	}
	v.signals(j.Signals)
	for i, q := range j.Questions {
		at := fmt.Sprintf("/questions/%d", i)
		v.check(q.Line >= 0, at+"/line", "line %d is less than 1", q.Line)
		v.check(q.Line == 0 || q.Path != "", at+"/path", "a question about a line names its file")
		v.check(!blank(q.Text), at+"/text", "the question is empty")
	}
	v.check(len(j.Notes) <= MaxNotes, "/notes", "%d notes, at most %d", len(j.Notes), MaxNotes)
	for i, n := range j.Notes {
		at := fmt.Sprintf("/notes/%d", i)
		v.check(!blank(n), at, "the note is empty")
		v.check(utf8.RuneCountInString(n) <= MaxNoteLength, at, "%d characters, at most %d", utf8.RuneCountInString(n), MaxNoteLength)
	}
	for i, s := range j.NotVerified {
		v.check(!blank(s), fmt.Sprintf("/notVerified/%d", i), "the entry is empty")
	}
	if len(v.errs) > 0 {
		return v.errs
	}
	return nil
}

type validator struct {
	review Review
	errs   ValidationError
}

func (v *validator) add(pointer, format string, args ...any) {
	v.errs = append(v.errs, FieldError{Pointer: pointer, Message: fmt.Sprintf(format, args...)})
}

func (v *validator) check(ok bool, pointer, format string, args ...any) {
	if !ok {
		v.add(pointer, format, args...)
	}
}

func blank(s string) bool { return strings.TrimSpace(s) == "" }

func (v *validator) finding(at string, f JudgedFinding, preExisting bool) {
	_, known := v.review.Rules[f.Rule]
	v.check(known, at+"/rule", "rule %q is not a rule of this change request", f.Rule)
	v.check(f.Severity.Valid(), at+"/severity", "unknown severity %q", string(f.Severity))
	file, changed := v.review.file(f.Path)
	v.check(changed, at+"/path", "%s is not a changed file of this change request", f.Path)
	sideOK := f.Side.Valid() && (!changed || file.Diff.Status.HasSide(f.Side))
	switch {
	case !f.Side.Valid():
		v.add(at+"/side", "unknown side %q", string(f.Side))
	case !sideOK:
		v.add(at+"/side", "%s is %s: it has no %s side", f.Path, file.Diff.Status, f.Side)
	}
	lines, linesKnown := file.lines(f.Side)
	switch {
	case f.Line < 1:
		v.add(at+"/line", "line %d is less than 1", f.Line)
	case changed && sideOK && linesKnown && f.Line > len(lines):
		v.add(at+"/line", "line %d is past the end of %s on the %s side (%d lines)", f.Line, f.Path, f.Side, len(lines))
	}
	switch {
	case f.StartLine < 0:
		v.add(at+"/startLine", "startLine %d is less than 1", f.StartLine)
	case f.StartLine > f.Line:
		v.add(at+"/startLine", "startLine %d is after line %d", f.StartLine, f.Line)
	}
	v.check(!blank(f.Title) && !strings.ContainsAny(f.Title, "\r\n"), at+"/title", "the title must be one non-empty line")
	v.check(!blank(f.Why), at+"/why", "why is empty: say what goes wrong")
	v.check(!blank(f.Fix), at+"/fix", "fix is empty: a finding without a fix is not a finding")
	switch {
	case f.Crash && !preExisting:
		v.add(at+"/crash", "crash is only for pre-existing defects")
	case f.Crash && f.Severity != Blocker:
		v.add(at+"/crash", "a crash is a BLOCKER, not %s", f.Severity)
	}
	for i, ref := range f.References {
		rat := fmt.Sprintf("%s/references/%d", at, i)
		v.check(projectKey.MatchString(ref.Project), rat+"/project", "%q is not a project key such as github:github.com/owner/repo", ref.Project)
		v.check(noSpace.MatchString(ref.Ref), rat+"/ref", "the ref must be a branch, tag or commit without spaces")
		v.check(relativePath.MatchString(ref.Path), rat+"/path", "%q is not a relative path", ref.Path)
		v.check(ref.Line >= 1, rat+"/line", "line %d is less than 1", ref.Line)
	}
	for _, s := range v.review.Signals {
		if s.Rule == f.Rule && s.Path == f.Path && f.Side == Right && s.Line == f.Line {
			v.add(at, "repeats signal %s; decide the signal instead", s.ID)
		}
	}
}

func (v *validator) signals(decisions []SignalDecision) {
	byID := map[string]Signal{}
	for _, s := range v.review.Signals {
		byID[s.ID] = s
	}
	decided := map[string]bool{}
	for i, d := range decisions {
		at := fmt.Sprintf("/signals/%d", i)
		s, known := byID[d.ID]
		switch {
		case !known:
			v.add(at+"/id", "no signal %q in the work package", d.ID)
		case decided[d.ID]:
			v.add(at+"/id", "signal %s is decided twice", d.ID)
		}
		decided[d.ID] = true
		switch d.Decision {
		case Confirm:
			severity := d.Severity
			switch {
			case d.Severity != "" && !d.Severity.Valid():
				v.add(at+"/severity", "unknown severity %q", string(d.Severity))
			case d.Severity == "" && known && s.NeedsJudgement:
				v.add(at+"/severity", "signal %s needs judgement: confirm it with a severity", d.ID)
			case d.Severity == "":
				severity = s.Severity
			}
			switch {
			case d.Crash && known && s.Origin != OriginExisting:
				v.add(at+"/crash", "crash is only for pre-existing defects; signal %s is on a line of the change request", d.ID)
			case d.Crash && known && severity.Valid() && severity != Blocker:
				v.add(at+"/crash", "a crash is a BLOCKER, not %s", severity)
			}
		case Dismiss:
			v.check(d.Severity == "", at+"/severity", "a dismissed signal has no severity")
			v.check(!d.Crash, at+"/crash", "a dismissed signal is not a crash")
			v.check(!blank(d.Reason), at+"/reason", "say why signal %s is dismissed", d.ID)
		default:
			v.add(at+"/decision", "unknown decision %q, want confirm or dismiss", string(d.Decision))
		}
	}
	for _, s := range v.review.Signals {
		if s.NeedsJudgement && !decided[s.ID] {
			v.add("/signals", "signal %s (%s at %s:%d) needs judgement: confirm or dismiss it", s.ID, s.Rule, s.Path, s.Line)
		}
	}
}
