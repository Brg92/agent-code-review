package lexer

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// position returns the 1-based line and rune column of a byte offset.
func position(src string, offset int) (line, col int) {
	line, col = 1, 1
	for i := 0; i < offset; {
		r, w := utf8.DecodeRuneInString(src[i:])
		switch {
		case r == '\n', r == '\r' && !strings.HasPrefix(src[i+w:], "\n"):
			line, col = line+1, 1
		default:
			col++
		}
		i += w
	}
	return line, col
}

// checkTokens checks that tokens lie in order within [from, to) of src,
// that each one's text and position match the source, and, at the top
// level, that only whitespace lies between them.
func checkTokens(t *testing.T, src string, toks []Token, from, to int, top bool) {
	t.Helper()
	pos := from
	for _, tok := range toks {
		end := tok.Offset + len(tok.Text)
		if tok.Offset < pos || end > to || tok.Text == "" {
			t.Fatalf("token %s %q at %d is out of order or empty (from %d, to %d)", tok.Kind, tok.Text, tok.Offset, pos, to)
		}
		if src[tok.Offset:end] != tok.Text {
			t.Fatalf("token %q at %d: source has %q", tok.Text, tok.Offset, src[tok.Offset:end])
		}
		if line, col := position(src, tok.Offset); line != tok.Line || col != tok.Column {
			t.Fatalf("token %q at %d: line %d column %d, want %d %d", tok.Text, tok.Offset, tok.Line, tok.Column, line, col)
		}
		if top && strings.TrimFunc(src[pos:tok.Offset], unicode.IsSpace) != "" {
			t.Fatalf("source %q before token %q is not whitespace", src[pos:tok.Offset], tok.Text)
		}
		checkTokens(t, src, tok.Inner, tok.Offset, end, false)
		pos = end
	}
	if top && strings.TrimFunc(src[pos:], unicode.IsSpace) != "" {
		t.Fatalf("source %q after the last token is not whitespace", src[pos:])
	}
}

// FuzzLexer checks, for both pack tables, that the lexer never panics,
// that its tokens cover the source but whitespace with exact texts and
// positions, and that Clean keeps the shape of every line. The seeds are
// the sources of the table-driven cases, the legacy ones included.
func FuzzLexer(f *testing.F) {
	for _, cases := range [][]tokenCase{tokenizeKotlinLegacyCases, tokenizeKotlinCases, tokenizeTemplatesLegacyCases, tokenizeJavaCases, tokenizeJavaHasNoTemplatesCases} {
		for _, c := range cases {
			f.Add(c.src)
		}
	}
	for _, cases := range [][]cleanCase{cleanKotlinLegacyCases, cleanKotlinCases, cleanJavaCases} {
		for _, c := range cases {
			f.Add(c.src)
		}
	}
	var tables []*Table
	for _, language := range []string{"kotlin", "java"} {
		tab, err := LoadTable([]byte(packTable(f, language)))
		if err != nil {
			f.Fatal(err)
		}
		tables = append(tables, tab)
	}
	f.Fuzz(func(t *testing.T, src string) {
		for _, tab := range tables {
			checkTokens(t, src, Tokenize(tab, src), 0, len(src), true)
			checkShape(t, src, Clean(tab, src))
		}
	})
}
