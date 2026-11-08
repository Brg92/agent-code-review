package core

import (
	"fmt"
	"sort"
)

// Finding is a finding after finalization: where it is published and its
// fingerprint. Its JSON form is the reportedFinding of
// schemas/findings.schema.json.
type Finding struct {
	// ID is the fingerprint of the finding.
	ID         string      `json:"id"`
	Rule       string      `json:"rule"`
	Severity   Severity    `json:"severity"`
	Path       string      `json:"path"`
	Side       Side        `json:"side"`
	Line       int         `json:"line"`
	StartLine  int         `json:"startLine,omitempty"`
	Title      string      `json:"title"`
	Why        string      `json:"why"`
	Fix        string      `json:"fix"`
	Suggestion *string     `json:"suggestion,omitempty"`
	References []Reference `json:"references"`
	Crash      bool        `json:"crash,omitempty"`
	Placement  Placement   `json:"placement"`
	// Anchor is set exactly when Placement is Inline.
	Anchor *Anchor `json:"anchor,omitempty"`
	// Signal is the id of the signal the finding was made from; empty for
	// a finding the agent added.
	Signal string `json:"-"`
}

// DismissedSignal is a signal the judgement dismissed, kept for the report.
type DismissedSignal struct {
	Signal Signal
	Reason string
}

// Result is a finalized change request: its findings, and what the report
// shows besides them.
type Result struct {
	CR      Key
	HeadSHA string
	Verdict Verdict
	Summary string
	// Findings are introduced by the change request; PreExisting were
	// already on the target branch.
	Findings    []Finding
	PreExisting []Finding
	Dismissed   []DismissedSignal
	Questions   []Question
	Notes       []string
	NotVerified []string
}

// Finalize turns the signals of a review and the judgement of the agent
// into findings. The judgement must pass ValidateJudgement; its
// ValidationError is returned otherwise.
//
// A signal becomes a finding with the text of its rule unless the
// judgement dismisses it; a signal on an existing line becomes a
// pre-existing finding. The findings of the signals come first, in the
// order of the work package, then those of the judgement. Each finding is
// anchored on the diff, or placed in the summary when its file shows no
// line; a suggestion that does not replace lines of one hunk on the RIGHT
// side becomes a code block at the end of the fix. Each finding gets its
// fingerprint as id, and the result its verdict.
func Finalize(r Review, j Judgement) (Result, error) {
	if err := ValidateJudgement(r, j); err != nil {
		return Result{}, err
	}
	res := Result{CR: r.CR, HeadSHA: r.HeadSHA, Summary: j.Summary, Questions: j.Questions, Notes: j.Notes, NotVerified: j.NotVerified}
	decisions := map[string]SignalDecision{}
	for _, d := range j.Signals {
		decisions[d.ID] = d
	}
	for _, s := range r.Signals {
		d := decisions[s.ID]
		if d.Decision == Dismiss {
			res.Dismissed = append(res.Dismissed, DismissedSignal{Signal: s, Reason: d.Reason})
			continue
		}
		rule, ok := r.Rules[s.Rule]
		if !ok {
			return Result{}, fmt.Errorf("signal %s: rule %q is not a rule of this change request", s.ID, s.Rule)
		}
		if blank(rule.Title) || blank(rule.Why) || blank(rule.Fix) {
			return Result{}, fmt.Errorf("signal %s: rule %q has no fix, title or why for its findings", s.ID, s.Rule)
		}
		f := Finding{
			Rule: s.Rule, Severity: s.Severity, Path: s.Path, Side: Right, Line: s.Line,
			Title: rule.Title, Why: rule.Why, Fix: rule.Fix, References: []Reference{}, Crash: d.Crash, Signal: s.ID,
		}
		if d.Severity != "" {
			f.Severity = d.Severity
		}
		if s.Origin == OriginExisting {
			res.PreExisting = append(res.PreExisting, f)
		} else {
			res.Findings = append(res.Findings, f)
		}
	}
	res.Findings = append(res.Findings, judged(j.Findings)...)
	res.PreExisting = append(res.PreExisting, judged(j.PreExisting)...)

	all := make([]*Finding, 0, len(res.Findings)+len(res.PreExisting))
	for i := range res.Findings {
		all = append(all, &res.Findings[i])
	}
	for i := range res.PreExisting {
		all = append(all, &res.PreExisting[i])
	}
	for _, f := range all {
		file, _ := r.file(f.Path)
		place(f, file.Diff)
	}
	fingerprint(r, all)
	res.Verdict = ChangeRequestVerdict(res.Findings, res.PreExisting)
	return res, nil
}

func judged(list []JudgedFinding) []Finding {
	out := make([]Finding, 0, len(list))
	for _, f := range list {
		refs := append([]Reference{}, f.References...)
		out = append(out, Finding{
			Rule: f.Rule, Severity: f.Severity, Path: f.Path, Side: f.Side, Line: f.Line, StartLine: f.StartLine,
			Title: f.Title, Why: f.Why, Fix: f.Fix, Suggestion: f.Suggestion, References: refs, Crash: f.Crash,
		})
	}
	return out
}

// place anchors a finding and keeps its suggestion only where the platform
// can apply it.
func place(f *Finding, d FileDiff) {
	f.Anchor = d.Anchor(f.Side, f.Line)
	f.Placement = Inline
	if f.Anchor == nil {
		f.Placement = Summary
	}
	if f.Suggestion != nil && !d.SuggestionFits(f.Side, f.StartLine, f.Line) {
		f.Fix += "\n\n" + CodeBlock(*f.Suggestion)
		f.Suggestion = nil
	}
}

// fingerprint sets the id of each finding. The occurrence index of a line
// text counts the findings with the same rule, path and normalised text in
// side and line order, so it does not depend on the order of the lists.
func fingerprint(r Review, all []*Finding) {
	type key struct{ rule, path, text string }
	groups := map[key][]int{}
	texts := make([]string, len(all))
	var order []key
	for i, f := range all {
		texts[i] = NormalizeLine(lineText(r, f.Path, f.Side, f.Line))
		k := key{f.Rule, f.Path, texts[i]}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], i)
	}
	for _, k := range order {
		members := groups[k]
		sort.SliceStable(members, func(a, b int) bool {
			fa, fb := all[members[a]], all[members[b]]
			if fa.Side != fb.Side {
				return fa.Side == Left
			}
			if fa.Line != fb.Line {
				return fa.Line < fb.Line
			}
			return fa.StartLine < fb.StartLine
		})
		for n, i := range members {
			all[i].ID = FindingFingerprint(k.rule, k.path, k.text, n)
		}
	}
}

// lineText is the text of a line of a changed file: from the file version
// of the side when it is known, else from the diff, else empty.
func lineText(r Review, path string, side Side, line int) string {
	file, _ := r.file(path)
	if lines, ok := file.lines(side); ok && line >= 1 && line <= len(lines) {
		return lines[line-1]
	}
	if _, l, ok := file.Diff.lineAt(side, line); ok {
		return l.Text
	}
	return ""
}
