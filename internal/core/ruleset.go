package core

import (
	"github.com/Brg92/agent-code-review/internal/packs"
	"github.com/Brg92/agent-code-review/internal/rules"
)

// CoreSource is the source of the rules of the core catalog.
const CoreSource = "core"

// EffectiveRule is a rule that applies to a change request.
type EffectiveRule struct {
	ID string
	// Title is the rule's title with its options; for a repository rule,
	// its text.
	Title    string
	Severity Severity
	// Source is CoreSource, the id of the pack that defines the rule, or
	// RepositorySource.
	Source         string
	Group          string
	NeedsJudgement bool
	// Options are the resolved values of the overridable parameters of the
	// rule, by name; nil when it has none.
	Options map[string]int
	// Rule is the rule with its options; for a repository rule, a rule
	// made from it.
	Rule *rules.Rule
}

// RulesetInput is what the rules of one change request are settled from.
type RulesetInput struct {
	// Set holds every loaded pack and rule; its rules are those that
	// disabledRules and ruleOptions may name.
	Set *packs.Set
	// Active are the packs active on the repository, in activation order.
	Active     []*packs.Pack
	Profile    string
	Dimensions []packs.DimensionValue
	// Criteria are the repository criteria, read from the target branch.
	Criteria RepoCriteria
}

// Ruleset is the rules that apply to one change request and the parts of
// the repository criteria left out.
type Ruleset struct {
	// Rules are the core rules, then those of each active pack in
	// activation order, then the repository rules, each in catalog order.
	Rules []EffectiveRule
	// Ignored are the items the criteria already left out, then those
	// left out here.
	Ignored []IgnoredItem
	// Context is what the conditions of the rules were checked against:
	// the profile and repository dimensions, with the file dimensions
	// left to each file.
	Context rules.Context
}

// EffectiveRules settles the rules of a change request: the core rules,
// those of the active packs and the repository rules. The conditions on
// the profile and the repository dimensions are applied once; those on
// file dimensions, such as platform, still hold and are checked per file
// by the rule engine. Repository rules named in disabledRules are left
// out; a disabledRules entry for any other id and a rule option that the
// rule does not declare, or that is out of its bounds, are ignored and
// reported. The criteria are not changed.
func EffectiveRules(in RulesetInput) Ruleset {
	c := in.Criteria
	c.DisabledRules = append([]string(nil), c.DisabledRules...)
	c.RuleOptions = map[string]map[string]int{}
	for id, values := range in.Criteria.RuleOptions {
		c.RuleOptions[id] = map[string]int{}
		for name, v := range values {
			c.RuleOptions[id][name] = v
		}
	}
	var known []*rules.Rule
	if in.Set != nil {
		known = in.Set.Rules
	}
	rs := Ruleset{Ignored: append(append([]IgnoredItem(nil), in.Criteria.Ignored...), c.settle(known)...)}
	rs.Context = rules.Context{Profile: in.Profile, Dimensions: map[string]string{}, PerFile: map[string]bool{}}
	for _, d := range in.Dimensions {
		rs.Context.Dimensions[d.ID] = d.Value
	}
	for _, p := range in.Active {
		for _, d := range p.FileDimensions {
			rs.Context.PerFile[d.ID] = true
		}
	}
	add := func(source string, list []*rules.Rule) {
		for _, r := range rules.Applicable(list, rs.Context) {
			if values := c.RuleOptions[r.ID]; len(values) > 0 {
				if withOptions, err := r.WithOptions(values); err == nil {
					r = withOptions
				}
			}
			e := EffectiveRule{ID: r.ID, Title: r.Title, Severity: Severity(r.Severity), Source: source, Group: r.Group, NeedsJudgement: r.NeedsJudgement, Rule: r}
			for _, o := range r.Options() {
				if e.Options == nil {
					e.Options = map[string]int{}
				}
				e.Options[o.Name] = o.Default
			}
			rs.Rules = append(rs.Rules, e)
		}
	}
	add(CoreSource, rules.Core().Rules)
	for _, p := range in.Active {
		add(p.ID, p.Rules.Rules)
	}
	disabled := map[string]bool{}
	for _, id := range c.DisabledRules {
		disabled[id] = true
	}
	for _, r := range c.Rules {
		if disabled[r.ID] {
			continue
		}
		rule := &rules.Rule{ID: r.ID, Title: r.Text, Severity: string(r.Severity), Description: r.Text}
		rs.Rules = append(rs.Rules, EffectiveRule{ID: r.ID, Title: r.Text, Severity: r.Severity, Source: RepositorySource, Rule: rule})
	}
	return rs
}

// Checklist is the checklist of a change request: the pack and core rules
// in sections by group, then the repository rules.
type Checklist struct {
	Sections   []rules.Section
	Repository []EffectiveRule
}

// Checklist builds the checklist of the rule set with rules.Checklist.
func (s Ruleset) Checklist() Checklist {
	var plugin []*rules.Rule
	var cl Checklist
	for _, r := range s.Rules {
		if r.Source == RepositorySource {
			cl.Repository = append(cl.Repository, r)
			continue
		}
		plugin = append(plugin, r.Rule)
	}
	cl.Sections = rules.Checklist(plugin, s.Context)
	return cl
}

// ByID indexes the rules of the set by id, as the review of the change
// request holds them.
func (s Ruleset) ByID() map[string]*rules.Rule {
	out := make(map[string]*rules.Rule, len(s.Rules))
	for _, r := range s.Rules {
		out[r.ID] = r.Rule
	}
	return out
}
