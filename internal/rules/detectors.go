package rules

import (
	"unicode/utf8"

	"github.com/Brg92/agent-code-review/internal/lexer"
)

// lines splits a source into lines the way the lexer counts them: a line
// ends at \n, \r\n or a lone \r, which is not part of it.
func lines(src string) []string {
	var out []string
	start := 0
	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '\n':
			out = append(out, src[start:i])
			start = i + 1
		case '\r':
			out = append(out, src[start:i])
			if i+1 < len(src) && src[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}
	if start < len(src) {
		out = append(out, src[start:])
	}
	return out
}

// lineLength signals lines longer than max runes, at the first rune past
// the limit.
func lineLength(m *matcher, f File, max int, out *collector) {
	for i, l := range lines(f.Source) {
		if !m.step() {
			return
		}
		if utf8.RuneCountInString(l) > max {
			out.add(i+1, max+1)
		}
	}
}

// missingDoc signals declarations at top level or directly in a type body
// that have no doc comment before them. A declaration starts a line, may
// open with annotations and modifiers, and is signalled at its
// declaration word.
func missingDoc(m *matcher, d docParams, out *collector) {
	// The top-level code tokens, with their index among all tokens.
	var code []lexer.Token
	var index []int
	for i, t := range m.all {
		switch t.Kind {
		case lexer.LineComment, lexer.BlockComment, lexer.DocComment:
			continue
		}
		code, index = append(code, t), append(index, i)
	}
	var stack []bool // true: a type body
	pending := false // a type word was seen and its body is not open yet
	header := 0      // depth inside the header of a pending type
	handled := -1    // declaration word already handled by an earlier line
	for k, t := range code {
		if !m.step() {
			return
		}
		lineStart := k == 0 || code[k-1].Line != t.Line
		inScope := (len(stack) == 0 || stack[len(stack)-1]) && header == 0
		if lineStart && k > handled && inScope {
			if decl, check := d.declaration(code, k); decl >= 0 {
				handled = decl
				if check && !d.documented(m.all, index[k], index[decl]) {
					out.add(code[decl].Line, code[decl].Column)
				}
			}
		}
		isWord := t.Kind == lexer.Identifier || t.Kind == lexer.Keyword
		switch {
		case isWord && d.types[t.Text] && !(k > 0 && d.notAfter[code[k-1].Text]):
			pending = true
		case isWord && d.declarations[t.Text] && header == 0:
			pending = false
		case t.Kind != lexer.Punct:
		case t.Text == d.header.Open && (header > 0 || pending):
			header++
		case t.Text == d.header.Close && header > 0:
			header--
		case t.Text == d.body.Open:
			stack = append(stack, pending)
			pending, header = false, 0
		case t.Text == d.body.Close && len(stack) > 0:
			stack = stack[:len(stack)-1]
		}
	}
}

// declaration parses annotations, modifiers and a declaration word from
// code[k], and returns the index of the declaration word, or -1. check is
// false when a skip modifier or a skip annotation exempts it.
func (d docParams) declaration(code []lexer.Token, k int) (decl int, check bool) {
	isWord := func(i int) bool {
		return i < len(code) && (code[i].Kind == lexer.Identifier || code[i].Kind == lexer.Keyword)
	}
	isPunct := func(i int, text string) bool {
		return i < len(code) && code[i].Kind == lexer.Punct && code[i].Text == text
	}
	skip := false
	i := k
	for isPunct(i, d.annotation) {
		i++
		if !isWord(i) {
			return -1, false
		}
		skip = skip || d.skipAnnotations[code[i].Text]
		i++
		for (isPunct(i, ".") || isPunct(i, ":")) && isWord(i+1) {
			skip = skip || d.skipAnnotations[code[i+1].Text]
			i += 2
		}
		if isPunct(i, d.header.Open) {
			depth := 0
			for ; i < len(code); i++ {
				if isPunct(i, d.header.Open) {
					depth++
				} else if isPunct(i, d.header.Close) {
					if depth--; depth == 0 {
						i++
						break
					}
				}
			}
		}
	}
	for isWord(i) && (d.modifiers[code[i].Text] || d.skipModifiers[code[i].Text]) {
		skip = skip || d.skipModifiers[code[i].Text]
		i++
	}
	if !isWord(i) || !d.declarations[code[i].Text] {
		return -1, false
	}
	return i, !skip
}

// documented reports whether a doc comment comes right before all[from]
// or among the tokens up to all[to].
func (d docParams) documented(all []lexer.Token, from, to int) bool {
	if from > 0 && all[from-1].Kind == lexer.DocComment {
		return true
	}
	for i := from; i < to; i++ {
		if all[i].Kind == lexer.DocComment {
			return true
		}
	}
	return false
}
