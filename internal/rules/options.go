package rules

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Option is a parameter of the match of a rule that a repository may
// override, with the bounds of the values it may set. Default is the value
// in use: the catalog's, or the one WithOptions set.
type Option struct {
	Name     string
	Min, Max int
	Default  int
}

// optionMax is the only overridable parameter so far: max of line-length.
const optionMax = "max"

type (
	rawOptions struct {
		Max *rawBounds `json:"max"`
	}
	rawBounds struct {
		Min *int `json:"min"`
		Max *int `json:"max"`
	}
)

// ruleText is the text of a rule as the catalog writes it, before the
// values of the options replace their {<name>} placeholders.
type ruleText struct {
	title, description, why, fix string
}

// Options returns the overridable parameters of the rule, by name; nil
// when it has none.
func (r *Rule) Options() []Option {
	if len(r.options) == 0 {
		return nil
	}
	return append([]Option(nil), r.options...)
}

// WithOptions returns a copy of the rule with the given parameter values,
// its text naming them. It fails, leaving the rule as it is, for a
// parameter the rule does not declare or a value outside its bounds.
func (r *Rule) WithOptions(values map[string]int) (*Rule, error) {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	out := *r
	out.options = append([]Option(nil), r.options...)
	for _, name := range names {
		i := sort.Search(len(out.options), func(i int) bool { return out.options[i].Name >= name })
		if i == len(out.options) || out.options[i].Name != name {
			return nil, fmt.Errorf("rule %s has no option %q", r.ID, name)
		}
		o := &out.options[i]
		if v := values[name]; v < o.Min || v > o.Max {
			return nil, fmt.Errorf("rule %s: %s %d is outside %d-%d", r.ID, name, v, o.Min, o.Max)
		}
		o.Default = values[name]
	}
	if r.match != nil {
		m := *r.match
		out.match = &m
	}
	out.apply()
	return &out, nil
}

// apply puts the option values in the match and in the text of the rule.
func (r *Rule) apply() {
	pairs := make([]string, 0, 2*len(r.options))
	for _, o := range r.options {
		if o.Name == optionMax {
			r.match.maxLength = o.Default
		}
		pairs = append(pairs, "{"+o.Name+"}", fmt.Sprint(o.Default))
	}
	rep := strings.NewReplacer(pairs...)
	r.Title, r.Description = rep.Replace(r.text.title), rep.Replace(r.text.description)
	r.Why, r.Fix = rep.Replace(r.text.why), rep.Replace(r.text.fix)
}

// compileOptions checks the options of a rule against its match: each
// names a parameter its detector has, with bounds that hold the catalog
// value. The text may name only declared options.
func compileOptions(r *Rule, raw *rawOptions) error {
	if raw != nil {
		if r.match == nil {
			return errors.New("options need a match to override")
		}
		if raw.Max != nil {
			if r.match.detector != detectorLineLength {
				return errors.New("option max is only for the line-length detector")
			}
			o, err := compileBounds(optionMax, raw.Max, r.match.maxLength)
			if err != nil {
				return err
			}
			r.options = append(r.options, o)
		}
	}
	for _, name := range []string{optionMax} {
		declared := false
		for _, o := range r.options {
			declared = declared || o.Name == name
		}
		for _, text := range []string{r.text.title, r.text.description, r.text.why, r.text.fix} {
			if !declared && strings.Contains(text, "{"+name+"}") {
				return fmt.Errorf("the text names {%s}, which is not an option of the rule", name)
			}
		}
	}
	return nil
}

func compileBounds(name string, b *rawBounds, value int) (Option, error) {
	switch {
	case b.Min == nil || b.Max == nil:
		return Option{}, fmt.Errorf("option %s needs min and max", name)
	case *b.Min < 1 || *b.Max < 1:
		return Option{}, fmt.Errorf("option %s: min and max must be at least 1", name)
	case *b.Min > *b.Max:
		return Option{}, fmt.Errorf("option %s: bounds %d-%d are reversed", name, *b.Min, *b.Max)
	case value < *b.Min || value > *b.Max:
		return Option{}, fmt.Errorf("option %s: the catalog value %d is outside %d-%d", name, value, *b.Min, *b.Max)
	}
	return Option{Name: name, Min: *b.Min, Max: *b.Max, Default: value}, nil
}
