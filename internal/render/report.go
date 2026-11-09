// Package render writes what a review shows the user: the report of each
// change request, the summary of a cascade and the final message in chat.
// Everything is rendered from the findings documents alone, in English,
// with LF line endings and no trailing spaces, and the same input always
// gives the same output. The reports are never published.
package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Brg92/agent-code-review/internal/core"
)

// severities lists the severities from the most to the least serious.
var severities = []core.Severity{core.Blocker, core.Major, core.Minor, core.Info}

func severityRank(s core.Severity) int {
	for i, v := range severities {
		if v == s {
			return i
		}
	}
	return len(severities)
}

// sectionTitles say what publishing does with the findings of a severity.
var sectionTitles = map[core.Severity]string{
	core.Blocker: "inline notes on publish",
	core.Major:   "one issue on publish",
	core.Minor:   "one issue on publish",
	core.Info:    "report only",
}

// criteriaSources name where the criteria come from.
var criteriaSources = map[string]string{
	"overlay": "overlay .agent-code-review/",
	"legacy":  "legacy .github/skills",
	"default": "defaults",
}

const (
	// markerWidth is the width of the widest entry marker, [BLOCKER].
	markerWidth = len("[BLOCKER]")
	// indent starts the lines that continue an entry, under its location.
	indent = "  " + "          "
)

// Report renders the report of one change request in its full form.
func Report(version string, d core.Document) string {
	var b builder
	b.line(headerLine(version, d))
	b.line(packsLine(d.Criteria))
	b.line(fmt.Sprintf("Criteria: %s ＠%s", criteriaSources[d.Criteria.Source], d.BaseRef))
	b.line(fmt.Sprintf("Scope: %s · head %s", plural(len(d.Files), "changed file", "changed files"), head8(d.HeadSHA)))
	b.line("")

	findings := sorted(d.Findings)
	bySeverity := map[core.Severity][]core.Finding{}
	for _, f := range findings {
		bySeverity[f.Severity] = append(bySeverity[f.Severity], f)
	}
	for _, s := range severities {
		list := bySeverity[s]
		if s == core.Info && len(list) == 0 {
			continue
		}
		b.line(fmt.Sprintf("%s (%d) — %s", s, len(list), sectionTitles[s]))
		for _, f := range list {
			entry(&b, f)
		}
		b.line("")
	}

	existing := sorted(d.PreExisting)
	b.line(fmt.Sprintf("Existing code in touched files (%d) — not written in this change request", len(existing)))
	for _, g := range grouped(existing) {
		if len(g) == 1 {
			entry(&b, g[0])
			continue
		}
		groupEntry(&b, g)
	}
	b.line("")

	footer(&b, d)
	b.line("Verdict: " + verdictText(bySeverity[core.Blocker], existing))
	return b.String()
}

func headerLine(version string, d core.Document) string {
	return fmt.Sprintf("agent-code-review v%s — %s \"%s\"", version, ref(d.CR), oneLine(d.Title))
}

func packsLine(c core.Criteria) string {
	parts := []string{fmt.Sprintf("profile %s: %s", c.Profile, oneLine(c.ProfileReason))}
	ids := make([]string, 0, len(c.Dimensions))
	for id := range c.Dimensions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		v := c.Dimensions[id]
		parts = append(parts, fmt.Sprintf("%s %s: %s", id, v.Value, oneLine(v.Reason)))
	}
	return fmt.Sprintf("Packs: %s (%s)", strings.Join(c.Packs, ", "), strings.Join(parts, " · "))
}

// ref names a change request as <project>#<number>.
func ref(k core.Key) string { return fmt.Sprintf("%s#%d", k.Project, k.Number) }

func head8(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// sorted orders findings by severity, then path, then line.
func sorted(list []core.Finding) []core.Finding {
	out := append([]core.Finding(nil), list...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if ra, rb := severityRank(a.Severity), severityRank(b.Severity); ra != rb {
			return ra < rb
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Line < b.Line
	})
	return out
}

// marker is the entry marker of a severity, padded to the widest one.
func marker(s core.Severity) string {
	m := "[" + string(s) + "]"
	return m + strings.Repeat(" ", markerWidth-len(m))
}

// location is where a finding is: its line, and the anchor a relocated
// finding is published on.
func location(f core.Finding) string {
	loc := fmt.Sprintf("%s:%d", f.Path, f.Line)
	if f.Anchor != nil && f.Anchor.Kind == core.AnchorRelocated {
		loc += fmt.Sprintf(" (relocated to %d)", f.Anchor.Line)
	}
	return loc
}

func entry(b *builder, f core.Finding) {
	tags := "[" + f.Rule + "]"
	if f.Crash {
		tags += " [crash]"
	}
	b.line(fmt.Sprintf("  %s %s — %s  %s", marker(f.Severity), location(f), oneLine(f.Title), tags))
	b.line(indent + "why: " + oneLine(f.Why))
	fix := "fix: " + oneLine(f.Fix)
	if f.Suggestion != nil {
		fix += "   (suggestion)"
	}
	b.line(indent + fix)
}

// grouped turns sorted pre-existing findings into entries: occurrences of
// one rule in one file with the same severity, why and fix share an entry,
// except crashes, which are always listed one by one. The entries are in
// the order of their first finding.
func grouped(existing []core.Finding) [][]core.Finding {
	type key struct {
		rule, path, why, fix string
		severity             core.Severity
	}
	var out [][]core.Finding
	at := map[key]int{}
	for _, f := range existing {
		k := key{f.Rule, f.Path, f.Why, f.Fix, f.Severity}
		if i, ok := at[k]; ok && !f.Crash {
			out[i] = append(out[i], f)
			continue
		}
		if !f.Crash {
			at[k] = len(out)
		}
		out = append(out, []core.Finding{f})
	}
	return out
}

// groupEntry writes occurrences of one rule in one file as one entry,
// with their count and every line. The title is that of the first.
func groupEntry(b *builder, g []core.Finding) {
	f := g[0]
	lines := make([]string, len(g))
	for i, o := range g {
		lines[i] = fmt.Sprint(o.Line)
	}
	b.line(fmt.Sprintf("  %s %s — %s  [%s] (%d)", marker(f.Severity), f.Path, oneLine(f.Title), f.Rule, len(g)))
	b.line(indent + "lines: " + strings.Join(lines, ", "))
	b.line(indent + "why: " + oneLine(f.Why))
	b.line(indent + "fix: " + oneLine(f.Fix))
}

func footer(b *builder, d core.Document) {
	if len(d.Questions) > 0 {
		b.line("Questions for the author:")
		for _, q := range d.Questions {
			at := ""
			switch {
			case q.Path != "" && q.Line > 0:
				at = fmt.Sprintf("%s:%d — ", q.Path, q.Line)
			case q.Path != "":
				at = q.Path + " — "
			}
			b.line("  - " + at + oneLine(q.Text))
		}
	}
	if len(d.SignalsDismissed) > 0 {
		b.line("Signals dismissed:")
		for _, s := range d.SignalsDismissed {
			b.line(fmt.Sprintf("  - %s %s:%d — %s", s.Rule, s.Path, s.Line, oneLine(s.Reason)))
		}
	}
	notVerified(b, d)
	if clean := cleanFiles(d); len(clean) > 0 {
		b.line("no findings: " + strings.Join(clean, ", "))
	}
	b.line("Pipeline: " + pipeline(d.CI))
	notes(b, d)
}

func notVerified(b *builder, d core.Document) {
	if len(d.NotVerified)+len(d.AgentNotVerified) == 0 {
		return
	}
	b.line("Not verified:")
	for _, n := range d.NotVerified {
		s := n.Path + " — " + n.Reason
		if strings.TrimSpace(n.Detail) != "" {
			s += ": " + oneLine(n.Detail)
		}
		b.line("  - " + s)
	}
	for _, s := range d.AgentNotVerified {
		b.line("  - " + oneLine(s))
	}
}

func notes(b *builder, d core.Document) {
	all := append(append([]string(nil), d.Criteria.Notes...), d.Notes...)
	for i, n := range all {
		lead := "Notes: "
		if i > 0 {
			lead = strings.Repeat(" ", len(lead))
		}
		b.line(lead + oneLine(n))
	}
}

// cleanFiles are the changed files, in diff order, that were reviewed and
// have no finding, new or pre-existing.
func cleanFiles(d core.Document) []string {
	skip := map[string]bool{}
	for _, f := range d.Findings {
		skip[f.Path] = true
	}
	for _, f := range d.PreExisting {
		skip[f.Path] = true
	}
	for _, n := range d.NotVerified {
		skip[n.Path] = true
	}
	var out []string
	for _, p := range d.Files {
		if !skip[p] {
			out = append(out, p)
		}
	}
	return out
}

// failedConclusions are the conclusions of a completed check that failed.
var failedConclusions = map[string]bool{"failure": true, "timed_out": true, "cancelled": true, "action_required": true}

// pipeline says how the checks on the head commit stand. A failing or
// pending pipeline names the checks that make it so, once each, in the
// order of the checks.
func pipeline(ci core.CI) string {
	switch ci.State {
	case core.CIPassing:
		return "passing on head (" + plural(len(ci.Checks), "check", "checks") + ")"
	case core.CIFailing:
		return named("failing on head", ci.Checks, func(c core.Check) bool {
			return c.Status == "completed" && c.Conclusion != nil && failedConclusions[*c.Conclusion]
		})
	case core.CIPending:
		return named("pending on head", ci.Checks, func(c core.Check) bool { return c.Status != "completed" })
	case core.CIUnknown:
		why := ci.Reason
		if d := oneLine(ci.Detail); d != "" {
			why += ": " + d
		}
		return "not verified (" + why + ")"
	}
	return "none on head"
}

func named(state string, checks []core.Check, pick func(core.Check) bool) string {
	var names []string
	seen := map[string]bool{}
	for _, c := range checks {
		if pick(c) && !seen[c.Name] {
			seen[c.Name] = true
			names = append(names, c.Name)
		}
	}
	if len(names) == 0 {
		return state
	}
	return state + ": " + strings.Join(names, ", ")
}

// verdictText is the verdict of the report, counted from the BLOCKER
// section and the crashes of the existing code section.
func verdictText(blockers, existing []core.Finding) string {
	crashes := 0
	for _, f := range existing {
		if f.Crash {
			crashes++
		}
	}
	var parts []string
	if len(blockers) > 0 {
		parts = append(parts, plural(len(blockers), "blocker", "blockers")+" in the change")
	}
	if crashes > 0 {
		parts = append(parts, plural(crashes, "crashing defect", "crashing defects")+" in existing code")
	}
	if len(parts) == 0 {
		return "OK — nothing blocks; non-blocking findings go to issues on publish."
	}
	return "BLOCKED — " + strings.Join(parts, " and ") + " to fix, then review again."
}

// plural writes a count with the singular or plural noun.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// oneLine joins the non-empty lines of a text with " · ", each without
// surrounding spaces.
func oneLine(s string) string {
	var parts []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			parts = append(parts, l)
		}
	}
	return strings.Join(parts, " · ")
}

// builder collects lines, each ended by "\n" and without trailing spaces.
type builder struct{ strings.Builder }

func (b *builder) line(s string) {
	b.WriteString(strings.TrimRight(s, " \t"))
	b.WriteByte('\n')
}
