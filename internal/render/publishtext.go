package render

import (
	"fmt"
	"strings"

	"github.com/Brg92/agent-code-review/internal/core"
)

// PlanView is one change request of a plan as the chat shows it.
type PlanView struct {
	CR    core.Key
	Title string
	Head  string
	Plan  core.Plan
	// Path is the plan file.
	Path string
	// Blocked is why nothing is written for the change request, if so.
	Blocked core.PlanStatus
	// Parent is the change request that lists this one, if any.
	Parent *core.Key
}

// PlanText renders what publish --plan prints: for each change request, in
// the order given, what would be written, with counts and rule ids, and
// what is skipped and why. It says that nothing was written.
func PlanText(runID string, choice core.Choice, forge core.Forge, views []PlanView) string {
	var b builder
	b.line(fmt.Sprintf("Plan for run %s (%s). Nothing was written to %s.", runID, choice, platforms[forge]))
	for _, v := range views {
		b.line("")
		b.line(fmt.Sprintf("%s — %s (head %s)", ref(v.CR), oneLine(v.Title), head8(v.Head)))
		writes := planWrites(v.Plan)
		for _, w := range writes {
			b.line("  write  " + w)
		}
		for _, s := range v.Plan.Skipped {
			b.line("  skip   " + string(s.Status) + ": " + skipText(s))
		}
		if len(writes) == 0 && v.Blocked == "" {
			b.line("  nothing to write")
		}
		b.line("  plan   " + v.Path)
	}
	b.line("")
	b.line("Next:")
	b.line("  agent-code-review publish --apply   write exactly these plans, only after the user said yes")
	b.line("  agent-code-review publish --plan <comments|issues|both>   plan again")
	return b.String()
}

// planWrites describes the actions of a plan in the order they run, with
// all the inline comments on one line, where the first of them is.
func planWrites(p core.Plan) []string {
	var comments []string
	for _, a := range p.Actions {
		if a.Kind == core.ActionReviewComment {
			comments = append(comments, a.Rules...)
		}
	}
	var out []string
	for _, a := range p.Actions {
		switch a.Kind {
		case core.ActionReviewComment:
			if len(out) == 0 || !strings.Contains(out[len(out)-1], "inline comment") {
				out = append(out, fmt.Sprintf("%s: %s", plural(len(comments), "inline comment", "inline comments"), ruleCounts(comments)))
			}
		case core.ActionLabelCreate:
			out = append(out, "label "+a.Label.Name)
		case core.ActionIssueCreate:
			out = append(out, fmt.Sprintf("issue %s (new): %s", a.Severity, ruleCounts(a.Rules)))
		case core.ActionIssueUpdate:
			out = append(out, fmt.Sprintf("issue %s (rewrite #%d): %s", a.Severity, a.Number, ruleCounts(a.Rules)))
		case core.ActionSummaryComment:
			out = append(out, "the note that nothing blocks")
		case core.ActionRelatedTick:
			out = append(out, "a tick in "+ref(*a.Parent))
		}
	}
	return out
}

// ruleCounts lists rule ids with how often each comes, in the order of the
// first.
func ruleCounts(rules []string) string {
	var order []string
	n := map[string]int{}
	for _, r := range rules {
		if n[r] == 0 {
			order = append(order, r)
		}
		n[r]++
	}
	parts := make([]string, len(order))
	for i, r := range order {
		parts[i] = r
		if n[r] > 1 {
			parts[i] += fmt.Sprintf(" ×%d", n[r])
		}
	}
	return strings.Join(parts, ", ")
}

// skipText describes a skipped entry: what it is about, where, and what
// the reader needs to know. The text that matched a secret pattern is never
// here, only the name of the pattern.
func skipText(s core.PlanSkip) string {
	what := s.Rule
	if what == "" {
		what = string(s.Kind)
	}
	if what == "" {
		what = s.Target
	}
	if s.Path != "" {
		what += fmt.Sprintf(" %s:%d", s.Path, s.Line)
	}
	if s.Pattern != "" {
		what += " (" + s.Pattern + ")"
	}
	if s.Detail != "" {
		what += " — " + oneLine(s.Detail)
	}
	if s.Target != "" && s.Kind != "" && s.Rule == "" && s.Detail == "" {
		what += " " + s.Target
	}
	return what
}

// ApplyView is one change request of an apply as the chat shows it.
type ApplyView struct {
	CR    core.Key
	Title string
	Head  string
	Plan  core.Plan
	// Outcomes are the outcomes the actions have, by id of the action.
	Outcomes map[int]core.Outcome
	// Blocked is why nothing was written for the change request, if so.
	Blocked core.PlanStatus
	Detail  string
}

// ApplyText renders what publish --apply prints: for each change request,
// what was written, what was not and why, and what is still to do. done says
// whether every action has a final outcome.
func ApplyText(runID string, forge core.Forge, views []ApplyView, done bool) string {
	var b builder
	if done {
		b.line(fmt.Sprintf("Published run %s to %s.", runID, platforms[forge]))
	} else {
		b.line(fmt.Sprintf("Run %s is only partly published to %s.", runID, platforms[forge]))
	}
	for _, v := range views {
		b.line("")
		b.line(fmt.Sprintf("%s — %s (head %s)", ref(v.CR), oneLine(v.Title), head8(v.Head)))
		if v.Blocked != "" {
			b.line(fmt.Sprintf("  nothing was written: %s — %s", v.Blocked, oneLine(v.Detail)))
			continue
		}
		if len(v.Plan.Actions) == 0 {
			b.line("  nothing to write")
		}
		var todo []string
		for _, a := range v.Plan.Actions {
			o, ok := v.Outcomes[a.ID]
			desc := actionText(a)
			switch {
			case !ok:
				todo = append(todo, desc+" — not tried")
			case o.Status == core.StatusFailed:
				todo = append(todo, desc+" — failed: "+oneLine(o.Detail))
			default:
				line := "  " + string(o.Status) + "  " + desc
				if o.Number > 0 {
					line += fmt.Sprintf(" (#%d)", o.Number)
				}
				if o.Detail != "" {
					line += " — " + oneLine(o.Detail)
				}
				b.line(line)
			}
		}
		for _, t := range todo {
			b.line("  to do  " + t)
		}
	}
	if !done {
		b.line("")
		b.line("Run `agent-code-review publish --apply` again to do what is left; what was done is not done twice.")
	}
	return b.String()
}

// actionText describes one action in a line.
func actionText(a core.Action) string {
	switch a.Kind {
	case core.ActionReviewComment:
		return fmt.Sprintf("inline comment %s %s:%d", a.Rules[0], a.Path, a.Line)
	case core.ActionSummaryComment:
		return "the note that nothing blocks"
	case core.ActionIssueCreate:
		return fmt.Sprintf("issue %s (new)", a.Severity)
	case core.ActionIssueUpdate:
		return fmt.Sprintf("issue %s (rewrite #%d)", a.Severity, a.Number)
	case core.ActionLabelCreate:
		return "label " + a.Label.Name
	case core.ActionRelatedTick:
		return "tick in " + ref(*a.Parent)
	}
	return string(a.Kind)
}
