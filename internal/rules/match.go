package rules

import "github.com/Brg92/agent-code-review/internal/lexer"

// codeTokens returns the code tokens of a file in source order: comments
// are dropped, and the expressions of string templates follow their
// string token.
func codeTokens(toks []lexer.Token) []lexer.Token {
	var out []lexer.Token
	var walk func([]lexer.Token)
	walk = func(toks []lexer.Token) {
		for _, t := range toks {
			switch t.Kind {
			case lexer.LineComment, lexer.BlockComment, lexer.DocComment:
				continue
			case lexer.Template:
				walk(t.Inner)
				continue
			}
			out = append(out, t)
			if t.Kind == lexer.String || t.Kind == lexer.RawString {
				walk(t.Inner)
			}
		}
	}
	walk(toks)
	return out
}

func (t tokenTest) match(tok lexer.Token) bool {
	if t.kinds != 0 && t.kinds&(1<<tok.Kind) == 0 {
		return false
	}
	switch {
	case t.text != nil:
		return tok.Text == *t.text
	case t.oneOf != nil:
		return t.oneOf[tok.Text]
	case t.re != nil:
		return t.re.MatchString(tok.Text)
	}
	return true
}

// maxDepth bounds the nesting of the matcher, which grows with the
// tokens a single match covers.
const maxDepth = 10000

// matcher runs patterns over the code tokens of one file within a step
// budget. Once the budget is spent every match fails and exceeded is set.
type matcher struct {
	all      []lexer.Token     // every token, comments included
	toks     []lexer.Token     // code tokens
	opens    map[string]string // opener -> closer
	closers  map[string]bool
	steps    int
	budget   int
	depth    int
	exceeded bool
}

func newMatcher(all, toks []lexer.Token, table *lexer.Table, budget int) *matcher {
	m := &matcher{all: all, toks: toks, opens: map[string]string{}, closers: map[string]bool{}, budget: budget}
	for _, b := range table.Brackets {
		m.opens[b.Open] = b.Close
		m.closers[b.Close] = true
	}
	return m
}

// step spends one step and reports whether the budget still holds.
func (m *matcher) step() bool {
	m.steps++
	if m.steps > m.budget || m.depth > maxDepth {
		m.exceeded = true
	}
	return !m.exceeded
}

// span is one match: tokens [start, end), and the token it points at.
type span struct{ start, end, at int }

// find returns the matches of a pattern starting at every token.
func (m *matcher) find(p pattern) []span {
	var out []span
	for i := range m.toks {
		if s, ok := m.matchAt(p, i); ok {
			out = append(out, s)
		}
		if m.exceeded {
			return nil
		}
	}
	return out
}

// matchAt matches a pattern starting at token i.
func (m *matcher) matchAt(p pattern, i int) (span, bool) {
	s := span{start: i, at: -1}
	ok := m.seq(p.elems, i, &s.at, func(end int) bool {
		s.end = end
		return true
	})
	if s.at < 0 {
		s.at = i
	}
	return s, ok && !m.exceeded
}

// seq matches elems from pos and calls k with the end of the match,
// backtracking until k accepts.
func (m *matcher) seq(elems []elem, pos int, at *int, k func(int) bool) bool {
	if len(elems) == 0 {
		return k(pos)
	}
	m.depth++
	defer func() { m.depth-- }()
	if !m.step() {
		return false
	}
	e := &elems[0]
	next := func(p int) bool { return m.seq(elems[1:], p, at, k) }
	switch e.op {
	case opToken:
		if pos >= len(m.toks) || !e.tok.match(m.toks[pos]) {
			return false
		}
		if !e.at {
			return next(pos + 1)
		}
		old := *at
		*at = pos
		if next(pos + 1) {
			return true
		}
		*at = old
		return false
	case opGap:
		return m.gap(e, pos, next)
	case opRepeat:
		return m.repeat(e, 0, pos, at, next)
	case opAnyOf:
		for _, alt := range e.alts {
			if m.seq(alt, pos, at, next) {
				return true
			}
			if m.exceeded {
				return false
			}
		}
	}
	return false
}

// gap skips as few tokens as needed for k to accept.
func (m *matcher) gap(e *elem, pos int, k func(int) bool) bool {
	var open []string
	for skip := 0; ; skip++ {
		if len(open) == 0 && k(pos+skip) {
			return true
		}
		p := pos + skip
		if m.exceeded || skip == e.gapMax || p >= len(m.toks) || !m.step() {
			return false
		}
		t := m.toks[p]
		for _, s := range e.stopAt {
			if s.match(t) {
				return false
			}
		}
		if e.balanced && t.Kind == lexer.Punct {
			if closer, ok := m.opens[t.Text]; ok {
				open = append(open, closer)
			} else if m.closers[t.Text] {
				if len(open) == 0 || open[len(open)-1] != t.Text {
					return false
				}
				open = open[:len(open)-1]
			}
		}
	}
}

// repeat matches the group as many times as possible, between min and
// max, then backs off one repetition at a time.
func (m *matcher) repeat(e *elem, count, pos int, at *int, k func(int) bool) bool {
	if e.max < 0 || count < e.max {
		more := func(p int) bool { return p > pos && m.repeat(e, count+1, p, at, k) }
		if m.seq(e.group, pos, at, more) {
			return true
		}
		if m.exceeded {
			return false
		}
	}
	return count >= e.min && k(pos)
}
