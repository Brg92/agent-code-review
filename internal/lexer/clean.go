package lexer

import (
	"strings"
	"unicode/utf8"
)

// Clean returns src with every comment and the contents of every string
// and char literal replaced by spaces, so code can be searched without
// matching text inside literals. The delimiters of literals stay, template
// markers become spaces and template expressions stay as code. Line breaks
// are kept and each other rune becomes one space, so every line keeps its
// length in runes and every code token keeps its line and column; byte
// offsets move where a blanked rune takes more than one byte.
func Clean(t *Table, src string) string {
	var spans [][2]int
	for _, tok := range Tokenize(t, src) {
		spans = blankSpans(t, tok, spans)
	}
	if len(spans) == 0 {
		return src
	}
	var b strings.Builder
	b.Grow(len(src))
	pos := 0
	for _, sp := range spans {
		b.WriteString(src[pos:sp[0]])
		for i := sp[0]; i < sp[1]; {
			r, w := utf8.DecodeRuneInString(src[i:sp[1]])
			if r == '\n' || r == '\r' {
				b.WriteByte(src[i])
			} else {
				b.WriteByte(' ')
			}
			i += w
		}
		pos = sp[1]
	}
	b.WriteString(src[pos:])
	return b.String()
}

// blankSpans appends the byte spans of tok to blank, in source order.
func blankSpans(t *Table, tok Token, spans [][2]int) [][2]int {
	end := tok.Offset + len(tok.Text)
	add := func(from, to int) {
		if from < to {
			spans = append(spans, [2]int{from, to})
		}
	}
	switch tok.Kind {
	case LineComment, BlockComment, DocComment:
		add(tok.Offset, end)
	case Char:
		d := len(t.Char.Delimiter)
		add(tok.Offset+d, closedEnd(tok, end, d))
	case String, RawString:
		d := len(stringForm(t, tok).Delimiter)
		from := tok.Offset + d
		for _, tm := range tok.Inner {
			add(from, tm.Offset)
			spans = templateSpans(t, tm, spans)
			from = tm.Offset + len(tm.Text)
		}
		add(from, closedEnd(tok, end, d))
	}
	return spans
}

// templateSpans appends the spans of the markers of a template and of the
// literals and comments inside its expression.
func templateSpans(t *Table, tm Token, spans [][2]int) [][2]int {
	tt := t.Templates
	if !strings.HasPrefix(tm.Text, tt.Open) {
		return append(spans, [2]int{tm.Offset, tm.Offset + len(tt.Identifier)})
	}
	spans = append(spans, [2]int{tm.Offset, tm.Offset + len(tt.Open)})
	for _, tok := range tm.Inner {
		spans = blankSpans(t, tok, spans)
	}
	if end := tm.Offset + len(tm.Text); !tm.Unterminated {
		spans = append(spans, [2]int{end - len(tt.Close), end})
	}
	return spans
}

// closedEnd is the end of the contents of a literal: before its closing
// delimiter, or at its end if it has none.
func closedEnd(tok Token, end, delimiter int) int {
	if tok.Unterminated {
		return end
	}
	return end - delimiter
}

// stringForm finds the form of a string token the way the lexer does:
// the longest delimiter that starts it.
func stringForm(t *Table, tok Token) *StringForm {
	for i := range t.Strings {
		if strings.HasPrefix(tok.Text, t.Strings[i].Delimiter) {
			return &t.Strings[i]
		}
	}
	return &StringForm{}
}
