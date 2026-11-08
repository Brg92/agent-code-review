package rules

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/Brg92/agent-code-review/internal/lexer"
)

// Catalog is a loaded rules catalog. Its JSON form is defined by
// schemas/rules-catalog.schema.json.
type Catalog struct {
	Pack  string
	Rules []*Rule
}

// Rule is one rule of a catalog.
type Rule struct {
	ID             string
	Title          string
	Severity       string
	NeedsJudgement bool
	Description    string
	// Languages the rule applies to; empty means every language.
	Languages []string
	Tags      []string
	// When holds the conditions for the rule to apply to a repository:
	// the key ProfileKey lists profiles, any other key is a dimension of
	// the pack and lists its values. Every condition must hold.
	When map[string][]string

	match *match
}

// ProfileKey is the key of the profile condition in Rule.When.
const ProfileKey = "profile"

// Matched reports whether the rule has mechanical checks; a rule without
// them is only judged.
func (r *Rule) Matched() bool { return r.match != nil }

// Line selections of a match.
const (
	linesAll   = "all"
	linesAdded = "added"
)

// Test file modes.
const (
	testsSkip    = "skip"
	testsInclude = "include"
	testsOnly    = "only"
)

// Detectors.
const (
	detectorLineLength = "line-length"
	detectorMissingDoc = "missing-doc"
)

// match is the compiled match of a rule, with the catalog defaults
// applied.
type match struct {
	patterns    []pattern
	exclude     []pattern
	ignoreLines []pattern
	files       files
	addedOnly   bool
	detector    string
	maxLength   int
	doc         docParams
}

type files struct {
	include, exclude      []Glob
	tests                 string
	contains, notContains []pattern
}

type docParams struct {
	declarations, modifiers, skipModifiers, types, skipAnnotations map[string]bool
	notAfter                                                       map[string]bool
	annotation                                                     string
	header, body                                                   lexer.Bracket
}

// pattern is a compiled token pattern.
type pattern struct {
	languages map[string]bool // nil: every language
	elems     []elem
}

type op int

const (
	opToken op = iota + 1
	opGap
	opRepeat
	opAnyOf
)

type elem struct {
	op  op
	tok tokenTest
	at  bool
	// gap
	gapMax   int
	balanced bool
	stopAt   []tokenTest
	// repeat
	group    []elem
	min, max int // max < 0: no limit
	// anyOf
	alts [][]elem
}

// kindSet is a set of lexer kinds; zero means any kind.
type kindSet uint32

type tokenTest struct {
	kinds kindSet
	text  *string
	oneOf map[string]bool
	re    *regexp.Regexp
}

var kindNames = map[string]kindSet{
	"identifier": 1 << lexer.Identifier,
	"keyword":    1 << lexer.Keyword,
	"word":       1<<lexer.Identifier | 1<<lexer.Keyword,
	"number":     1 << lexer.Number,
	"string":     1 << lexer.String,
	"raw-string": 1 << lexer.RawString,
	"char":       1 << lexer.Char,
	"punct":      1 << lexer.Punct,
	"any":        0,
}

var (
	packID     = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	ruleID     = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*(?:\.[a-z0-9]+(?:-[a-z0-9]+)*)*$`)
	languageID = regexp.MustCompile(`^[a-z0-9-]+$`)
	tag        = regexp.MustCompile(`^[a-z0-9-]+$`)
	nonBlank   = regexp.MustCompile(`\S`)
	marker     = regexp.MustCompile(`^\S+$`)
	word       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	bracket    = regexp.MustCompile(`^[^\sA-Za-z0-9_]$`)
	// dimensionID is the form of a dimension and of its values.
	dimensionID = packID
	profiles    = map[string]bool{"app": true, "library": true, "kmp": true}
	severities  = map[string]bool{"BLOCKER": true, "MAJOR": true, "MINOR": true, "INFO": true}
)

// Raw JSON forms. Pointers tell an absent field from an empty one.
type (
	rawCatalog struct {
		Schema   *int        `json:"schema"`
		Pack     *string     `json:"pack"`
		Defaults *rawDefault `json:"defaults"`
		Rules    *[]rawRule  `json:"rules"`
	}
	rawDefault struct {
		Files       *rawFiles    `json:"files"`
		IgnoreLines []rawPattern `json:"ignoreLines"`
	}
	rawRule struct {
		ID             *string                    `json:"id"`
		Title          *string                    `json:"title"`
		Severity       *string                    `json:"severity"`
		NeedsJudgement bool                       `json:"needsJudgement"`
		Description    *string                    `json:"description"`
		Languages      *[]string                  `json:"languages"`
		Tags           *[]string                  `json:"tags"`
		When           map[string]json.RawMessage `json:"when"`
		Match          *rawMatch                  `json:"match"`
	}
	rawMatch struct {
		Patterns        []rawPattern `json:"patterns"`
		Exclude         []rawPattern `json:"exclude"`
		Files           *rawFiles    `json:"files"`
		Lines           *string      `json:"lines"`
		Detector        *string      `json:"detector"`
		Max             *int         `json:"max"`
		Declarations    []string     `json:"declarations"`
		Modifiers       []string     `json:"modifiers"`
		SkipModifiers   []string     `json:"skipModifiers"`
		Types           []string     `json:"types"`
		NotAfter        []string     `json:"notAfter"`
		Annotation      *string      `json:"annotation"`
		SkipAnnotations []string     `json:"skipAnnotations"`
		Header          *rawBracket  `json:"header"`
		Body            *rawBracket  `json:"body"`
	}
	rawBracket struct {
		Open  string `json:"open"`
		Close string `json:"close"`
	}
	rawFiles struct {
		Include     *[]string     `json:"include"`
		Exclude     []string      `json:"exclude"`
		Tests       *string       `json:"tests"`
		Contains    *[]rawPattern `json:"contains"`
		NotContains []rawPattern  `json:"notContains"`
	}
	rawPattern struct {
		Languages *[]string     `json:"languages"`
		Tokens    *[]rawElement `json:"tokens"`
	}
	rawToken struct {
		Kind   *rawKind  `json:"kind"`
		Text   *string   `json:"text"`
		OneOf  *[]string `json:"oneOf"`
		Regexp *string   `json:"regexp"`
	}
	rawElement struct {
		rawToken
		At     *bool           `json:"at"`
		Gap    *rawGap         `json:"gap"`
		Repeat *[]rawElement   `json:"repeat"`
		Min    *int            `json:"min"`
		Max    *int            `json:"max"`
		AnyOf  *[][]rawElement `json:"anyOf"`
	}
	rawGap struct {
		Max      *int       `json:"max"`
		Balanced bool       `json:"balanced"`
		StopAt   []rawToken `json:"stopAt"`
	}
)

// rawKind is a kind name or a list of kind names.
type rawKind []string

func (k *rawKind) UnmarshalJSON(data []byte) error {
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		*k = rawKind{one}
		return nil
	}
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return errors.New("kind must be a string or a list of strings")
	}
	if len(list) == 0 {
		return errors.New("kind list is empty")
	}
	*k = list
	return nil
}

// LoadCatalog decodes and checks a rules catalog. Unknown fields are
// rejected.
func LoadCatalog(data []byte) (*Catalog, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var raw rawCatalog
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("rules catalog: decode: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("rules catalog: trailing data after the document")
	}
	c, err := compileCatalog(raw)
	if err != nil {
		return nil, fmt.Errorf("rules catalog: %w", err)
	}
	return c, nil
}

func compileCatalog(raw rawCatalog) (*Catalog, error) {
	if raw.Schema == nil || *raw.Schema != 1 {
		return nil, errors.New("schema must be 1")
	}
	if raw.Pack == nil || !packID.MatchString(*raw.Pack) {
		return nil, errors.New("pack must be a lowercase pack id")
	}
	if raw.Rules == nil {
		return nil, errors.New("rules is required")
	}
	var defFiles files
	var ignore []pattern
	if d := raw.Defaults; d != nil {
		var err error
		if defFiles, err = compileFiles(d.Files, nil); err != nil {
			return nil, fmt.Errorf("defaults: %w", err)
		}
		if ignore, err = compilePatterns(d.IgnoreLines, nil, false); err != nil {
			return nil, fmt.Errorf("defaults: ignoreLines: %w", err)
		}
	} else {
		defFiles.tests = testsSkip
	}
	c := &Catalog{Pack: *raw.Pack}
	seen := map[string]bool{}
	for i, rr := range *raw.Rules {
		r, err := compileRule(rr, defFiles, ignore)
		if err != nil {
			return nil, fmt.Errorf("rule %d: %w", i+1, err)
		}
		if seen[r.ID] {
			return nil, fmt.Errorf("rule %q is listed twice", r.ID)
		}
		seen[r.ID] = true
		c.Rules = append(c.Rules, r)
	}
	return c, nil
}

func compileRule(rr rawRule, defFiles files, ignore []pattern) (*Rule, error) {
	if rr.ID == nil || !ruleID.MatchString(*rr.ID) {
		return nil, errors.New("id must be a lowercase rule id")
	}
	r := &Rule{ID: *rr.ID, NeedsJudgement: rr.NeedsJudgement}
	fail := func(format string, args ...any) (*Rule, error) {
		return nil, fmt.Errorf("%q: "+format, append([]any{r.ID}, args...)...)
	}
	for name, v := range map[string]*string{"title": rr.Title, "description": rr.Description} {
		if v == nil || !nonBlank.MatchString(*v) {
			return fail("%s is required", name)
		}
	}
	if rr.Severity == nil || !severities[*rr.Severity] {
		return fail("severity must be BLOCKER, MAJOR, MINOR or INFO")
	}
	r.Title, r.Description, r.Severity = *rr.Title, *rr.Description, *rr.Severity
	if rr.Languages == nil {
		return fail("languages is required")
	}
	if rr.Tags == nil {
		return fail("tags is required")
	}
	r.Languages, r.Tags = *rr.Languages, *rr.Tags
	for _, l := range r.Languages {
		if !languageID.MatchString(l) {
			return fail("language %q is not a language id", l)
		}
	}
	for _, t := range r.Tags {
		if !tag.MatchString(t) {
			return fail("tag %q is not lowercase", t)
		}
	}
	var err error
	if r.When, err = compileWhen(rr.When); err != nil {
		return fail("when: %v", err)
	}
	if rr.Match != nil {
		m, err := compileMatch(*rr.Match, r.Languages, defFiles, ignore)
		if err != nil {
			return fail("match: %v", err)
		}
		r.match = m
	}
	return r, nil
}

func compileWhen(raw map[string]json.RawMessage) (map[string][]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	when := map[string][]string{}
	for key, data := range raw {
		if !dimensionID.MatchString(key) {
			return nil, fmt.Errorf("%q is not a profile or a dimension id", key)
		}
		var values []string
		if err := json.Unmarshal(data, &values); err != nil {
			return nil, fmt.Errorf("%s must be a list of strings", key)
		}
		if len(values) == 0 {
			return nil, fmt.Errorf("%s is empty", key)
		}
		seen := map[string]bool{}
		for _, v := range values {
			switch {
			case !dimensionID.MatchString(v):
				return nil, fmt.Errorf("%s: %q is not a lowercase value", key, v)
			case seen[v]:
				return nil, fmt.Errorf("%s: %q is listed twice", key, v)
			case key == ProfileKey && !profiles[v]:
				return nil, fmt.Errorf("profile %q must be app, library or kmp", v)
			}
			seen[v] = true
		}
		when[key] = values
	}
	return when, nil
}

func compileMatch(rm rawMatch, languages []string, defFiles files, ignore []pattern) (*match, error) {
	m := &match{ignoreLines: ignore, files: defFiles}
	if rm.Files != nil {
		f, err := compileFiles(rm.Files, languages)
		if err != nil {
			return nil, err
		}
		m.files = f
	}
	if rm.Lines != nil {
		switch *rm.Lines {
		case linesAll:
		case linesAdded:
			m.addedOnly = true
		default:
			return nil, fmt.Errorf("lines %q must be all or added", *rm.Lines)
		}
	}
	docFields := map[string]bool{
		"declarations": rm.Declarations != nil, "modifiers": rm.Modifiers != nil, "skipModifiers": rm.SkipModifiers != nil,
		"types": rm.Types != nil, "notAfter": rm.NotAfter != nil, "annotation": rm.Annotation != nil,
		"skipAnnotations": rm.SkipAnnotations != nil, "header": rm.Header != nil, "body": rm.Body != nil,
	}
	forbid := func(names ...string) error {
		for _, n := range names {
			if docFields[n] {
				return fmt.Errorf("%s is only for the missing-doc detector", n)
			}
		}
		return nil
	}
	allDoc := []string{"declarations", "modifiers", "skipModifiers", "types", "notAfter", "annotation", "skipAnnotations", "header", "body"}
	if rm.Detector == nil {
		if rm.Max != nil {
			return nil, errors.New("max is only for the line-length detector")
		}
		if err := forbid(allDoc...); err != nil {
			return nil, err
		}
		if len(rm.Patterns) == 0 {
			return nil, errors.New("patterns is required without a detector")
		}
		var err error
		if m.patterns, err = compilePatterns(rm.Patterns, languages, true); err != nil {
			return nil, fmt.Errorf("patterns: %w", err)
		}
		if m.exclude, err = compilePatterns(rm.Exclude, languages, false); err != nil {
			return nil, fmt.Errorf("exclude: %w", err)
		}
		return m, nil
	}
	if rm.Patterns != nil || rm.Exclude != nil {
		return nil, errors.New("a detector takes no patterns or exclude")
	}
	m.detector = *rm.Detector
	switch m.detector {
	case detectorLineLength:
		if rm.Max == nil || *rm.Max < 1 {
			return nil, errors.New("line-length needs a max of at least 1")
		}
		if err := forbid(allDoc...); err != nil {
			return nil, err
		}
		m.maxLength = *rm.Max
	case detectorMissingDoc:
		if rm.Max != nil {
			return nil, errors.New("max is only for the line-length detector")
		}
		d, err := compileDoc(rm)
		if err != nil {
			return nil, err
		}
		m.doc = d
	default:
		return nil, fmt.Errorf("unknown detector %q", m.detector)
	}
	return m, nil
}

func compileDoc(rm rawMatch) (docParams, error) {
	var d docParams
	if len(rm.Declarations) == 0 || len(rm.Types) == 0 {
		return d, errors.New("missing-doc needs declarations and types")
	}
	if rm.Annotation == nil || !marker.MatchString(*rm.Annotation) {
		return d, errors.New("missing-doc needs an annotation marker")
	}
	if rm.Header == nil || rm.Body == nil {
		return d, errors.New("missing-doc needs header and body brackets")
	}
	words := func(name string, list []string) (map[string]bool, error) {
		set := map[string]bool{}
		for _, w := range list {
			if !word.MatchString(w) || set[w] {
				return nil, fmt.Errorf("%s: %q is not a distinct word", name, w)
			}
			set[w] = true
		}
		return set, nil
	}
	var err error
	for _, f := range []struct {
		name string
		list []string
		dst  *map[string]bool
	}{
		{"declarations", rm.Declarations, &d.declarations}, {"modifiers", rm.Modifiers, &d.modifiers},
		{"skipModifiers", rm.SkipModifiers, &d.skipModifiers}, {"types", rm.Types, &d.types},
		{"skipAnnotations", rm.SkipAnnotations, &d.skipAnnotations},
	} {
		if *f.dst, err = words(f.name, f.list); err != nil {
			return d, err
		}
	}
	d.notAfter = map[string]bool{}
	for _, p := range rm.NotAfter {
		if !marker.MatchString(p) {
			return d, fmt.Errorf("notAfter: %q is not a marker", p)
		}
		d.notAfter[p] = true
	}
	for name, b := range map[string]*rawBracket{"header": rm.Header, "body": rm.Body} {
		if !bracket.MatchString(b.Open) || !bracket.MatchString(b.Close) || b.Open == b.Close {
			return d, fmt.Errorf("%s: not a pair of distinct punctuation characters", name)
		}
	}
	d.annotation = *rm.Annotation
	d.header = lexer.Bracket{Open: rm.Header.Open, Close: rm.Header.Close}
	d.body = lexer.Bracket{Open: rm.Body.Open, Close: rm.Body.Close}
	return d, nil
}

func compileFiles(rf *rawFiles, languages []string) (files, error) {
	f := files{tests: testsSkip}
	if rf == nil {
		return f, nil
	}
	globs := func(name string, list []string) ([]Glob, error) {
		var out []Glob
		for _, s := range list {
			g, err := CompileGlob(s)
			if err != nil {
				return nil, fmt.Errorf("files: %s: %w", name, err)
			}
			out = append(out, g)
		}
		return out, nil
	}
	var err error
	if rf.Include != nil {
		if len(*rf.Include) == 0 {
			return f, errors.New("files: include is empty")
		}
		if f.include, err = globs("include", *rf.Include); err != nil {
			return f, err
		}
	}
	if f.exclude, err = globs("exclude", rf.Exclude); err != nil {
		return f, err
	}
	if rf.Tests != nil {
		switch *rf.Tests {
		case testsSkip, testsInclude, testsOnly:
			f.tests = *rf.Tests
		default:
			return f, fmt.Errorf("files: tests %q must be skip, include or only", *rf.Tests)
		}
	}
	if rf.Contains != nil {
		if len(*rf.Contains) == 0 {
			return f, errors.New("files: contains is empty")
		}
		if f.contains, err = compilePatterns(*rf.Contains, languages, false); err != nil {
			return f, fmt.Errorf("files: contains: %w", err)
		}
	}
	if f.notContains, err = compilePatterns(rf.NotContains, languages, false); err != nil {
		return f, fmt.Errorf("files: notContains: %w", err)
	}
	return f, nil
}

// compilePatterns compiles patterns whose languages must be among the
// given ones (any, if none are given). at reports whether the patterns
// may mark the token a signal points at.
func compilePatterns(raws []rawPattern, languages []string, at bool) ([]pattern, error) {
	allowed := map[string]bool{}
	for _, l := range languages {
		allowed[l] = true
	}
	var out []pattern
	for i, rp := range raws {
		var p pattern
		if rp.Languages != nil {
			if len(*rp.Languages) == 0 {
				return nil, fmt.Errorf("pattern %d: languages is empty", i+1)
			}
			p.languages = map[string]bool{}
			for _, l := range *rp.Languages {
				if !languageID.MatchString(l) || (len(allowed) > 0 && !allowed[l]) {
					return nil, fmt.Errorf("pattern %d: language %q is not a language of the rule", i+1, l)
				}
				p.languages[l] = true
			}
		}
		if rp.Tokens == nil || len(*rp.Tokens) == 0 {
			return nil, fmt.Errorf("pattern %d: tokens is empty", i+1)
		}
		ats := 0
		elems, err := compileElements(*rp.Tokens, &ats, true)
		if err != nil {
			return nil, fmt.Errorf("pattern %d: %w", i+1, err)
		}
		if ats > 1 {
			return nil, fmt.Errorf("pattern %d: at is set on more than one token", i+1)
		}
		if ats == 1 && !at {
			return nil, fmt.Errorf("pattern %d: at is only for the patterns of a rule", i+1)
		}
		p.elems = elems
		out = append(out, p)
	}
	return out, nil
}

// compileElements compiles a sequence. ats counts the tokens marked at;
// atAllowed is false inside a repeated group.
func compileElements(raws []rawElement, ats *int, atAllowed bool) ([]elem, error) {
	var out []elem
	for i, re := range raws {
		e, err := compileElement(re, ats, atAllowed)
		if err != nil {
			return nil, fmt.Errorf("element %d: %w", i+1, err)
		}
		out = append(out, e)
	}
	return out, nil
}

func compileElement(re rawElement, ats *int, atAllowed bool) (elem, error) {
	isToken := re.Kind != nil || re.Text != nil || re.OneOf != nil || re.Regexp != nil || re.At != nil
	forms := 0
	for _, f := range []bool{re.Gap != nil, re.Repeat != nil, re.AnyOf != nil} {
		if f {
			forms++
		}
	}
	if forms > 1 || (forms == 1 && isToken) {
		return elem{}, errors.New("an element is one of a token, a gap, a repeat or an anyOf")
	}
	if re.Repeat == nil && (re.Min != nil || re.Max != nil) {
		return elem{}, errors.New("min and max are only for repeat")
	}
	switch {
	case re.Gap != nil:
		e := elem{op: opGap, gapMax: 64, balanced: re.Gap.Balanced}
		if re.Gap.Max != nil {
			if *re.Gap.Max < 0 {
				return elem{}, errors.New("gap max is negative")
			}
			e.gapMax = *re.Gap.Max
		}
		for _, rt := range re.Gap.StopAt {
			t, err := compileToken(rt)
			if err != nil {
				return elem{}, fmt.Errorf("gap stopAt: %w", err)
			}
			e.stopAt = append(e.stopAt, t)
		}
		return e, nil
	case re.Repeat != nil:
		if len(*re.Repeat) == 0 {
			return elem{}, errors.New("repeat is empty")
		}
		group, err := compileElements(*re.Repeat, ats, false)
		if err != nil {
			return elem{}, fmt.Errorf("repeat: %w", err)
		}
		e := elem{op: opRepeat, group: group, max: -1}
		if re.Min != nil {
			if *re.Min < 0 {
				return elem{}, errors.New("repeat min is negative")
			}
			e.min = *re.Min
		}
		if re.Max != nil {
			if *re.Max < 1 || *re.Max < e.min {
				return elem{}, errors.New("repeat max must be at least 1 and at least min")
			}
			e.max = *re.Max
		}
		return e, nil
	case re.AnyOf != nil:
		if len(*re.AnyOf) < 2 {
			return elem{}, errors.New("anyOf needs at least two alternatives")
		}
		e := elem{op: opAnyOf}
		for i, alt := range *re.AnyOf {
			if len(alt) == 0 {
				return elem{}, fmt.Errorf("anyOf alternative %d is empty", i+1)
			}
			altAts := 0
			seq, err := compileElements(alt, &altAts, atAllowed)
			if err != nil {
				return elem{}, fmt.Errorf("anyOf alternative %d: %w", i+1, err)
			}
			if altAts > 1 {
				return elem{}, fmt.Errorf("anyOf alternative %d: at is set on more than one token", i+1)
			}
			if altAts == 1 && *ats == 0 {
				*ats = 1 // alternatives are exclusive: one at per path
			} else if altAts == 1 {
				*ats = 2
			}
			e.alts = append(e.alts, seq)
		}
		return e, nil
	}
	t, err := compileToken(re.rawToken)
	if err != nil {
		return elem{}, err
	}
	e := elem{op: opToken, tok: t}
	if re.At != nil {
		if !*re.At {
			return elem{}, errors.New("at must be true when set")
		}
		if !atAllowed {
			return elem{}, errors.New("at is not allowed inside a repeat")
		}
		e.at = true
		*ats++
	}
	return e, nil
}

func compileToken(rt rawToken) (tokenTest, error) {
	var t tokenTest
	if rt.Kind != nil {
		for _, k := range *rt.Kind {
			set, ok := kindNames[k]
			if !ok {
				return t, fmt.Errorf("unknown kind %q", k)
			}
			if set == 0 {
				t.kinds = 0
				break
			}
			t.kinds |= set
		}
	}
	n := 0
	if rt.Text != nil {
		n++
		if !nonBlank.MatchString(*rt.Text) {
			return t, errors.New("text is blank")
		}
		t.text = rt.Text
	}
	if rt.OneOf != nil {
		n++
		if len(*rt.OneOf) == 0 {
			return t, errors.New("oneOf is empty")
		}
		t.oneOf = map[string]bool{}
		for _, s := range *rt.OneOf {
			if !nonBlank.MatchString(s) || t.oneOf[s] {
				return t, fmt.Errorf("oneOf: %q is blank or listed twice", s)
			}
			t.oneOf[s] = true
		}
	}
	if rt.Regexp != nil {
		n++
		re, err := regexp.Compile(*rt.Regexp)
		if err != nil || strings.TrimSpace(*rt.Regexp) == "" {
			return t, fmt.Errorf("regexp %q does not compile", *rt.Regexp)
		}
		t.re = re
	}
	if n > 1 {
		return t, errors.New("at most one of text, oneOf and regexp")
	}
	return t, nil
}
