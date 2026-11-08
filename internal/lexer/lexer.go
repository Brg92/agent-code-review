// Package lexer splits source code into tokens, driven by a language table
// loaded at runtime, so the core needs no knowledge of any language.
//
// The lexer never fails: malformed input yields tokens flagged as
// unterminated. A string or character literal that may not span lines ends
// at the end of its line; a block comment or multiline string ends at the
// end of the file.
package lexer

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Kind is the kind of a token.
type Kind int

// Token kinds. A soft keyword is an Identifier with SoftKeyword set.
const (
	Identifier Kind = iota + 1
	Keyword
	Number
	String
	RawString
	Char
	Template
	LineComment
	BlockComment
	DocComment
	Punct
)

var kindNames = [...]string{
	Identifier:   "Identifier",
	Keyword:      "Keyword",
	Number:       "Number",
	String:       "String",
	RawString:    "RawString",
	Char:         "Char",
	Template:     "Template",
	LineComment:  "LineComment",
	BlockComment: "BlockComment",
	DocComment:   "DocComment",
	Punct:        "Punct",
}

func (k Kind) String() string {
	if k > 0 && int(k) < len(kindNames) {
		return kindNames[k]
	}
	return "Kind(" + strconv.Itoa(int(k)) + ")"
}

// Token is one token of the source.
type Token struct {
	Kind Kind
	// Text is the exact source text of the token, delimiters included.
	Text string
	// Offset is the byte offset of the token in the source.
	Offset int
	// Line and Column are 1-based; Column counts runes. A line ends at
	// \n, \r\n or a lone \r.
	Line, Column int
	// SoftKeyword marks an Identifier that is a soft keyword.
	SoftKeyword bool
	// Unterminated marks a literal, comment or template that has no
	// closing delimiter.
	Unterminated bool
	// Inner holds the templates of a string, and the tokens of the
	// expression of a template.
	Inner []Token
}

// Tokenize splits src into tokens, skipping whitespace. The table must come
// from LoadTable.
func Tokenize(t *Table, src string) []Token {
	s := &scanner{t: t, src: src, line: 1, col: 1}
	return s.code()
}

type scanner struct {
	t         *Table
	src       string
	pos       int
	line, col int
}

func (s *scanner) eof() bool { return s.pos >= len(s.src) }

func (s *scanner) peek(prefix string) bool {
	return prefix != "" && strings.HasPrefix(s.src[s.pos:], prefix)
}

func (s *scanner) rune() rune {
	r, _ := utf8.DecodeRuneInString(s.src[s.pos:])
	return r
}

// atLineEnd reports whether the scanner is at a line break.
func (s *scanner) atLineEnd() bool {
	return !s.eof() && (s.src[s.pos] == '\n' || s.src[s.pos] == '\r')
}

// advance moves over n bytes, which must end on a rune boundary, keeping
// line and column.
func (s *scanner) advance(n int) {
	for end := s.pos + n; s.pos < end; {
		r, w := utf8.DecodeRuneInString(s.src[s.pos:])
		s.pos += w
		switch {
		case r == '\n', r == '\r' && !s.peek("\n"):
			s.line, s.col = s.line+1, 1
		default:
			s.col++
		}
	}
}

// advanceRune moves over one rune.
func (s *scanner) advanceRune() {
	_, w := utf8.DecodeRuneInString(s.src[s.pos:])
	s.advance(w)
}

// start begins a token at the current position.
func (s *scanner) start(kind Kind) Token {
	return Token{Kind: kind, Offset: s.pos, Line: s.line, Column: s.col}
}

// end sets the text of a token that runs to the current position.
func (s *scanner) end(tok Token) Token {
	tok.Text = s.src[tok.Offset:s.pos]
	return tok
}

// code scans tokens up to the end of the source.
func (s *scanner) code() []Token {
	var toks []Token
	for !s.eof() {
		if unicode.IsSpace(s.rune()) {
			s.advanceRune()
			continue
		}
		toks = append(toks, s.next())
	}
	return toks
}

// next scans the token at the current position, which is not whitespace.
func (s *scanner) next() Token {
	t := s.t
	if b := t.BlockComment; b != nil && s.peek(b.Open) {
		return s.blockComment()
	}
	if s.peek(t.LineComment) {
		tok := s.start(LineComment)
		for !s.eof() && !s.atLineEnd() {
			s.advanceRune()
		}
		return s.end(tok)
	}
	for i := range t.Strings {
		if s.peek(t.Strings[i].Delimiter) {
			return s.stringLiteral(&t.Strings[i])
		}
	}
	if c := t.Char; c != nil && s.peek(c.Delimiter) {
		return s.charLiteral(c)
	}
	if s.peek(t.QuotedIdentifier) {
		return s.quotedIdentifier()
	}
	r := s.rune()
	if s.identStart(r) {
		return s.identifier()
	}
	if isDigit(r) || r == '.' && s.pos+1 < len(s.src) && isDigit(rune(s.src[s.pos+1])) {
		return s.number()
	}
	tok := s.start(Punct)
	for _, op := range t.Operators {
		if s.peek(op) {
			s.advance(len(op))
			return s.end(tok)
		}
	}
	s.advanceRune()
	return s.end(tok)
}

func (s *scanner) blockComment() Token {
	b := s.t.BlockComment
	tok := s.start(BlockComment)
	if s.peek(s.t.DocComment) && !strings.HasPrefix(s.src[s.pos+len(b.Open):], b.Close) {
		tok.Kind = DocComment
	}
	s.advance(len(b.Open))
	for depth := 1; depth > 0; {
		switch {
		case s.eof():
			tok.Unterminated = true
			return s.end(tok)
		case b.Nested && s.peek(b.Open):
			s.advance(len(b.Open))
			depth++
		case s.peek(b.Close):
			s.advance(len(b.Close))
			depth--
		default:
			s.advanceRune()
		}
	}
	return s.end(tok)
}

func (s *scanner) stringLiteral(f *StringForm) Token {
	tok := s.start(String)
	if f.Raw {
		tok.Kind = RawString
	}
	s.advance(len(f.Delimiter))
	for {
		switch {
		case s.eof(), !f.Multiline && s.atLineEnd():
			tok.Unterminated = true
			return s.end(tok)
		case s.peek(f.Escape):
			s.escape(f.Escape, f.Multiline)
		case s.peek(f.Delimiter):
			s.advance(len(f.Delimiter))
			if f.GreedyClose {
				_, w := utf8.DecodeRuneInString(f.Delimiter)
				for s.peek(f.Delimiter[:w]) {
					s.advance(w)
				}
			}
			return s.end(tok)
		default:
			s.advanceRune()
		}
	}
}

// escape moves over an escape character and the character it escapes. A
// line break is escaped only where the literal may span lines.
func (s *scanner) escape(escape string, multiline bool) {
	s.advance(len(escape))
	if !s.eof() && (multiline || !s.atLineEnd()) {
		s.advanceRune()
	}
}

func (s *scanner) charLiteral(c *CharForm) Token {
	tok := s.start(Char)
	s.advance(len(c.Delimiter))
	for {
		switch {
		case s.eof(), s.atLineEnd():
			tok.Unterminated = true
			return s.end(tok)
		case s.peek(c.Escape):
			s.escape(c.Escape, false)
		case s.peek(c.Delimiter):
			s.advance(len(c.Delimiter))
			return s.end(tok)
		default:
			s.advanceRune()
		}
	}
}

func (s *scanner) quotedIdentifier() Token {
	q := s.t.QuotedIdentifier
	tok := s.start(Identifier)
	s.advance(len(q))
	for {
		switch {
		case s.eof(), s.atLineEnd():
			tok.Unterminated = true
			return s.end(tok)
		case s.peek(q):
			s.advance(len(q))
			return s.end(tok)
		default:
			s.advanceRune()
		}
	}
}

func (s *scanner) identStart(r rune) bool {
	return unicode.IsLetter(r) || r == '_' || strings.ContainsRune(s.t.IdentifierChars, r)
}

func (s *scanner) identifier() Token {
	tok := s.start(Identifier)
	for !s.eof() {
		if r := s.rune(); !s.identStart(r) && !unicode.IsDigit(r) {
			break
		}
		s.advanceRune()
	}
	tok = s.end(tok)
	switch {
	case s.t.keywords[tok.Text]:
		tok.Kind = Keyword
	case s.t.softKeywords[tok.Text]:
		tok.SoftKeyword = true
	}
	return tok
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

// number scans a numeric literal loosely: digits, letters and underscores,
// a dot followed by a digit, and a sign after an exponent marker.
func (s *scanner) number() Token {
	tok := s.start(Number)
	hex := s.peek("0x") || s.peek("0X")
	for !s.eof() {
		c := s.src[s.pos]
		var prev byte
		if s.pos > tok.Offset {
			prev = s.src[s.pos-1]
		}
		switch {
		case c == '.' && s.pos+1 < len(s.src) && isDigit(rune(s.src[s.pos+1])):
		case (c == '+' || c == '-') && (!hex && (prev == 'e' || prev == 'E') || hex && (prev == 'p' || prev == 'P')):
		case c < utf8.RuneSelf && (isDigit(rune(c)) || c == '_' || unicode.IsLetter(rune(c))):
		default:
			return s.end(tok)
		}
		s.advance(1)
	}
	return s.end(tok)
}
