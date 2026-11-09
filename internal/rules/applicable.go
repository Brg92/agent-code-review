package rules

// Context is what the conditions of rules are checked against: the
// profile of the repository and the value of each dimension of its packs.
type Context struct {
	Profile    string
	Dimensions map[string]string
	// PerFile holds the keys, such as the file dimensions, whose
	// conditions are left to each file: here they hold.
	PerFile map[string]bool
}

// AppliesIn reports whether the conditions of the rule hold in the
// context. A rule without conditions always applies; a condition on a
// profile or a dimension the context lacks does not hold, unless the key
// is one of PerFile.
func (r *Rule) AppliesIn(c Context) bool {
	for key, values := range r.When {
		if c.PerFile[key] {
			continue
		}
		have, ok := c.Dimensions[key]
		if key == ProfileKey {
			have, ok = c.Profile, c.Profile != ""
		}
		if !ok || !contains(values, have) {
			return false
		}
	}
	return true
}

// Applicable returns the rules that apply in the context, in order.
func Applicable(rules []*Rule, c Context) []*Rule {
	var out []*Rule
	for _, r := range rules {
		if r.AppliesIn(c) {
			out = append(out, r)
		}
	}
	return out
}

// Section is a part of a checklist: the rules of one group.
type Section struct {
	// Heading is the group; empty for the rules that have none.
	Heading string
	Rules   []*Rule
}

// Checklist returns the rules that apply in the context grouped by their
// group. Sections follow the order in which each group first appears in
// rules, and so do the rules in each section.
func Checklist(rules []*Rule, c Context) []Section {
	var sections []Section
	at := map[string]int{}
	for _, r := range Applicable(rules, c) {
		i, ok := at[r.Group]
		if !ok {
			i = len(sections)
			at[r.Group] = i
			sections = append(sections, Section{Heading: r.Group})
		}
		sections[i].Rules = append(sections[i].Rules, r)
	}
	return sections
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}
