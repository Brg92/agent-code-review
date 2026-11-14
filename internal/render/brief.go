package render

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Brg92/agent-code-review/internal/core"
)

// JudgementSchema is the contract of the judgement the brief asks for.
const JudgementSchema = "schemas/judgement.schema.json"

// Brief renders part n, from 1, of a work package as the compact text the
// agent judges from. Part 1 has the header, the checklist, the repository
// rules, the criteria ignored, the cascade and the change-request
// signals; every part has its files, each with its diff, its code with
// line numbers and its signals. The brief is not stored: the same work
// package always gives the same text.
func Brief(wp core.WorkPackage, n int) (string, error) {
	part, ok := wp.Part(n)
	if !ok {
		return "", fmt.Errorf("brief: no part %d: the work package of %s has %s", n, wp.CR, plural(wp.Parts.Total, "part", "parts"))
	}
	var b builder
	b.line(fmt.Sprintf("Work package %s — part %d of %d", wp.CR, part.Number, part.Total))
	b.line(fmt.Sprintf("Title: %s", oneLine(wp.Title)))
	b.line(fmt.Sprintf("Head: %s · base %s at %s", wp.HeadSHA, wp.BaseRef, head8(wp.BaseSHA)))
	if part.Number == 1 {
		briefHeader(&b, wp)
	}
	if len(part.Signals)+len(part.ChangeRequestSignals) > 0 {
		briefSignalsHelp(&b)
	}
	if part.Number == 1 && len(part.ChangeRequestSignals) > 0 {
		b.line("")
		b.line("Change-request signals")
		for _, s := range part.ChangeRequestSignals {
			b.line(briefSignal(s, true))
		}
	}
	b.line("")
	b.line(fmt.Sprintf("Files in this part: %d of %d", len(part.Files), len(wp.Files)))
	for _, f := range part.Files {
		briefFile(&b, f, part.Signals)
	}
	b.line("")
	b.line(fmt.Sprintf("Judgement: write %s as defined by %s.", wp.JudgementPath, JudgementSchema))
	return b.String(), nil
}

func briefHeader(b *builder, wp core.WorkPackage) {
	b.line("URL: " + wp.URL)
	if d := strings.TrimSpace(wp.Description); d != "" {
		b.line("Description:")
		for _, l := range strings.Split(strings.ReplaceAll(d, "\r\n", "\n"), "\n") {
			b.line("  " + l)
		}
	}
	c := wp.Criteria
	b.line(fmt.Sprintf("Profile: %s (%s)", c.Profile, oneLine(c.ProfileReason)))
	ids := make([]string, 0, len(c.Dimensions))
	for id := range c.Dimensions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		b.line(fmt.Sprintf("Dimension: %s %s (%s)", id, c.Dimensions[id].Value, oneLine(c.Dimensions[id].Reason)))
	}
	criteria := fmt.Sprintf("Criteria: %s on %s", c.Source, wp.BaseRef)
	if len(c.Files) > 0 {
		criteria += ": " + strings.Join(c.Files, ", ")
	}
	b.line(fmt.Sprintf("Packs: %s", strings.Join(c.Packs, ", ")))
	b.line(criteria)
	if len(wp.Cascade.Ancestors) == 0 {
		b.line("Cascade: root")
	} else {
		var chain []string
		for _, a := range wp.Cascade.Ancestors {
			chain = append(chain, fmt.Sprintf("%s \"%s\"", a.CR, oneLine(a.Title)))
		}
		b.line("Cascade: below " + strings.Join(chain, " > "))
	}
	for _, r := range wp.RelatedChanges {
		line := fmt.Sprintf("Related: %s \"%s\"", r.CR, oneLine(r.Title))
		if r.WorkPackage != "" {
			line += " (" + r.WorkPackage + ")"
		}
		b.line(line)
	}

	rules := map[string]core.PackageRule{}
	for _, r := range wp.Rules {
		rules[r.ID] = r
	}
	b.line("")
	b.line("Checklist")
	for _, s := range wp.Checklist.Sections {
		heading := s.Heading
		if heading == "" {
			heading = "(no group)"
		}
		b.line("  " + heading)
		for _, id := range s.Rules {
			b.line("    " + briefRule(rules[id]))
		}
	}
	b.line("")
	b.line("Repository rules")
	if len(wp.Checklist.Repository) == 0 {
		b.line("  none")
	}
	for _, id := range wp.Checklist.Repository {
		b.line("  " + briefRule(rules[id]))
	}
	if len(c.Ignored) > 0 {
		b.line("")
		b.line("Repository criteria ignored")
		for _, i := range c.Ignored {
			b.line(fmt.Sprintf("  %s — %s", oneLine(i.Item), oneLine(i.Reason)))
		}
	}
	if len(wp.NotVerified) > 0 {
		b.line("")
		b.line("Not verified")
		for _, nv := range wp.NotVerified {
			line := fmt.Sprintf("  %s: %s", nv.Path, nv.Reason)
			if nv.Detail != "" {
				line += " (" + oneLine(nv.Detail) + ")"
			}
			b.line(line)
		}
	}
}

// briefRule writes a rule of the checklist: its id, severity, whether its
// signals need a decision, its options and its title.
func briefRule(r core.PackageRule) string {
	tags := []string{string(r.Severity)}
	if r.NeedsJudgement {
		tags = append(tags, "needs judgement")
	}
	names := make([]string, 0, len(r.Options))
	for name := range r.Options {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		tags = append(tags, fmt.Sprintf("%s %d", name, r.Options[name]))
	}
	return fmt.Sprintf("%s [%s] %s", r.ID, strings.Join(tags, ", "), oneLine(r.Title))
}

// briefSignalsHelp states how the judgement treats the signals, as
// core.ValidateJudgement and core.Finalize do.
func briefSignalsHelp(b *builder) {
	b.line("")
	b.line("Signals: [DECIDE] needs a decision: confirm it with a severity, or dismiss it with a reason.")
	b.line("  [<SEVERITY>] becomes a finding at that severity unless dismissed with a reason; a confirmation keeps the severity.")
	b.line("  existing: the line is already on the target branch, so its finding is pre-existing; only a pre-existing signal")
	b.line("  can be confirmed as a crash, and a crash is a BLOCKER.")
	b.line("  Decide a signal; do not repeat it as a finding.")
}

// briefSignal writes a signal; with path, the place names its file.
func briefSignal(s core.Signal, path bool) string {
	mark := "[" + string(s.Severity) + "]"
	if s.NeedsJudgement {
		mark = "[DECIDE]"
	}
	at := fmt.Sprintf("%d:%d", s.Line, s.Column)
	if path {
		at = s.Path + ":" + at
	}
	if s.Side == core.Left {
		at += " LEFT"
	}
	line := fmt.Sprintf("  %s %s %s at %s, %s", s.ID, mark, s.Rule, at, s.Origin)
	for _, r := range s.References {
		line += fmt.Sprintf(" · uses %s@%s %s:%d", r.Project, r.Ref, r.Path, r.Line)
	}
	return line
}

func briefFile(b *builder, f core.PackageFile, signals []core.Signal) {
	b.line("")
	desc := []string{string(f.Status)}
	if f.PreviousPath != "" {
		desc = append(desc, "from "+f.PreviousPath)
	}
	if f.Language != "" {
		desc = append(desc, f.Language)
	}
	if f.Placement == core.Summary {
		desc = append(desc, "findings in the summary: "+f.Reason)
	}
	b.line(fmt.Sprintf("== %s (%s)", f.Path, strings.Join(desc, ", ")))
	if f.Diff != "" {
		b.line("diff:")
		for _, l := range strings.Split(strings.TrimSuffix(f.Diff, "\n"), "\n") {
			b.line(l)
		}
	}
	if f.Code != nil {
		kind := "regions"
		if f.Code.Whole {
			kind = "whole file"
		}
		b.line("code at head (" + kind + "):")
		last := f.Code.Regions[len(f.Code.Regions)-1]
		width := len(strconv.Itoa(last.Start + len(last.Lines) - 1))
		for i, r := range f.Code.Regions {
			if i > 0 {
				b.line(strings.Repeat(" ", width) + " ...")
			}
			for j, l := range r.Lines {
				b.line(fmt.Sprintf("%*d | %s", width, r.Start+j, l))
			}
		}
	}
	var own []core.Signal
	for _, s := range signals {
		if s.Path == f.Path {
			own = append(own, s)
		}
	}
	if len(own) > 0 {
		b.line("signals:")
		for _, s := range own {
			b.line(briefSignal(s, false))
		}
	}
}
