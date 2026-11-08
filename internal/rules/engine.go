package rules

import (
	"sort"

	"github.com/Brg92/agent-code-review/internal/lexer"
)

// Origins of a signal.
const (
	OriginMR       = "mr"
	OriginExisting = "existing"
)

// DefaultBudget is the number of matching steps one rule may spend on one
// file when Options.Budget is zero.
const DefaultBudget = 2000000

// File is one file to check.
type File struct {
	// Path is relative to the repository, with forward slashes.
	Path     string
	Language string
	// Table is the lexer table of the language.
	Table  *lexer.Table
	Source string
	// Test reports whether the file is a test source of its pack.
	Test bool
	// Added holds the 1-based lines the change request adds.
	Added map[int]bool
}

// Signal is a place a rule points at.
type Signal struct {
	RuleID string `json:"ruleId"`
	// Severity is the rule's; empty when the rule needs judgement.
	Severity       string `json:"severity,omitempty"`
	NeedsJudgement bool   `json:"needsJudgement,omitempty"`
	Path           string `json:"path"`
	Line           int    `json:"line"`
	Column         int    `json:"column"`
	Origin         string `json:"origin"`
}

// NotVerified is a file a rule could not check.
type NotVerified struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

// Options tune a run.
type Options struct {
	// Budget is the number of matching steps one rule may spend on one
	// file; zero means DefaultBudget.
	Budget int
}

// Result is what a run found in one file.
type Result struct {
	Signals     []Signal
	NotVerified []NotVerified
}

// Run checks one file with the rules that have mechanical checks and apply
// to its language. Signals are sorted by line, column and rule order, one
// per rule and line.
func Run(rules []*Rule, f File, opt Options) Result {
	budget := opt.Budget
	if budget <= 0 {
		budget = DefaultBudget
	}
	code := codeTokens(lexer.Tokenize(f.Table, f.Source))
	var res Result
	order := map[string]int{}
	for i, r := range rules {
		if !r.Matched() || !r.appliesTo(f.Language) {
			continue
		}
		order[r.ID] = i
		m := newMatcher(code, f.Table, budget)
		res.Signals = append(res.Signals, r.run(m, f)...)
	}
	sort.SliceStable(res.Signals, func(i, j int) bool {
		a, b := res.Signals[i], res.Signals[j]
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		return order[a.RuleID] < order[b.RuleID]
	})
	return res
}

func (r *Rule) appliesTo(language string) bool {
	if len(r.Languages) == 0 {
		return true
	}
	for _, l := range r.Languages {
		if l == language {
			return true
		}
	}
	return false
}

func (p pattern) appliesTo(language string) bool {
	return p.languages == nil || p.languages[language]
}

// run applies the token patterns of a rule.
func (r *Rule) run(m *matcher, f File) []Signal {
	byLine := map[int]int{} // line -> index in sigs
	var sigs []Signal
	for _, p := range r.match.patterns {
		if !p.appliesTo(f.Language) {
			continue
		}
		for _, s := range m.find(p) {
			tok := m.toks[s.at]
			if i, ok := byLine[tok.Line]; ok {
				if tok.Column < sigs[i].Column {
					sigs[i].Column = tok.Column
				}
				continue
			}
			byLine[tok.Line] = len(sigs)
			sigs = append(sigs, r.signal(f, tok.Line, tok.Column))
		}
	}
	return sigs
}

func (r *Rule) signal(f File, line, column int) Signal {
	s := Signal{RuleID: r.ID, Severity: r.Severity, Path: f.Path, Line: line, Column: column, Origin: OriginExisting}
	if r.NeedsJudgement {
		s.Severity, s.NeedsJudgement = "", true
	}
	if f.Added[line] {
		s.Origin = OriginMR
	}
	return s
}
