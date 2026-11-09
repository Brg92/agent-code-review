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

// ReasonMatchBudget is the not-verified reason of a file a rule ran out
// of matching steps on; the detail names the rule.
const ReasonMatchBudget = "match-budget-exceeded"

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

// Result is what a run found: in one file for Run, in the whole change
// request for RunChangeRequest.
type Result struct {
	Signals     []Signal
	NotVerified []NotVerified
}

// Run checks one file with the rules that have mechanical checks on one
// file and apply to its language. Signals are sorted by line, column and rule order, one
// per rule and line. A rule that runs out of its step budget on the file
// stops there: its signals are dropped and the file is listed as not
// verified for it.
func Run(rules []*Rule, f File, opt Options) Result {
	budget := opt.Budget
	if budget <= 0 {
		budget = DefaultBudget
	}
	all := lexer.Tokenize(f.Table, f.Source)
	code := codeTokens(all)
	var res Result
	order := map[string]int{}
	for i, r := range rules {
		if !r.Matched() || r.OnChangeRequest() || !r.appliesTo(f.Language) || !r.match.files.selects(f) {
			continue
		}
		order[r.ID] = i
		m := newMatcher(all, code, f.Table, budget)
		sigs := r.run(m, f)
		if m.exceeded {
			res.NotVerified = append(res.NotVerified, NotVerified{Path: f.Path, Reason: ReasonMatchBudget, Detail: r.ID})
			continue
		}
		res.Signals = append(res.Signals, sigs...)
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

// selects reports whether the path and test conditions take the file.
func (fs files) selects(f File) bool {
	switch {
	case fs.tests == testsSkip && f.Test, fs.tests == testsOnly && !f.Test:
		return false
	}
	if fs.include != nil && !anyGlob(fs.include, f.Path) {
		return false
	}
	return !anyGlob(fs.exclude, f.Path)
}

func anyGlob(globs []Glob, path string) bool {
	for _, g := range globs {
		if g.Match(path) {
			return true
		}
	}
	return false
}

// found reports whether any of the patterns matches somewhere.
func found(m *matcher, f File, patterns []pattern) bool {
	for _, p := range patterns {
		if p.appliesTo(f.Language) && len(m.find(p)) > 0 {
			return true
		}
	}
	return false
}

// ignoredLines returns the lines whose first code token starts a match of
// one of the patterns.
func ignoredLines(m *matcher, f File, patterns []pattern) map[int]bool {
	ignored := map[int]bool{}
	for i, t := range m.toks {
		if i > 0 && m.toks[i-1].Line == t.Line || ignored[t.Line] {
			continue
		}
		for _, p := range patterns {
			if !p.appliesTo(f.Language) {
				continue
			}
			if _, ok := m.matchAt(p, i); ok {
				ignored[t.Line] = true
				break
			}
		}
	}
	return ignored
}

// run applies the token patterns of a rule.
func (r *Rule) run(m *matcher, f File) []Signal {
	mt := r.match
	if mt.files.contains != nil && !found(m, f, mt.files.contains) || found(m, f, mt.files.notContains) {
		return nil
	}
	out := &collector{rule: r, file: f, ignored: ignoredLines(m, f, mt.ignoreLines), byLine: map[int]int{}}
	switch mt.detector {
	case detectorLineLength:
		lineLength(m, f, mt.maxLength, out)
		return out.sigs
	case detectorMissingDoc:
		missingDoc(m, mt.doc, out)
		return out.sigs
	}
	var excluded []span
	for _, p := range mt.exclude {
		if p.appliesTo(f.Language) {
			excluded = append(excluded, m.find(p)...)
		}
	}
	for _, p := range mt.patterns {
		if !p.appliesTo(f.Language) {
			continue
		}
		for _, s := range m.find(p) {
			if !inside(s.at, excluded) {
				tok := m.toks[s.at]
				out.add(tok.Line, tok.Column)
			}
		}
	}
	return out.sigs
}

// collector gathers the signals of one rule on one file, one per line.
type collector struct {
	rule    *Rule
	file    File
	ignored map[int]bool
	byLine  map[int]int // line -> index in sigs
	sigs    []Signal
}

func (c *collector) add(line, column int) {
	if c.ignored[line] || c.rule.match.addedOnly && !c.file.Added[line] {
		return
	}
	if i, ok := c.byLine[line]; ok {
		if column < c.sigs[i].Column {
			c.sigs[i].Column = column
		}
		return
	}
	c.byLine[line] = len(c.sigs)
	c.sigs = append(c.sigs, c.rule.signal(c.file, line, column))
}

func inside(i int, spans []span) bool {
	for _, s := range spans {
		if i >= s.start && i < s.end {
			return true
		}
	}
	return false
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
